package commands

import (
    "fmt"
    "github.com/jawher/mow.cli"
    "log"
    "os"
    "os/exec"
    "os/user"
    "path"
    "strconv"
    "text/template"
    "io/ioutil"
    "database/sql"
    _ "github.com/mattn/go-sqlite3"
)

type Host struct {
    Domain        string
    ContainerName string
    ContainerPort int
    tpl80         *template.Template
    tpl443        *template.Template
    vhostConfig   string
}

func HostCmdEntry(cmd *cli.Cmd) {
    cmd.Command("add", "Add nginx vhost, and issue and install SSL certificate", hostAddCmdEntry)
    cmd.Command("del", "Remove nginx vhost, revoke and remove SSL certificate", hostDelCmdEntry)
    cmd.Command("ls", "List existing hosts with issued certs", hostLsCmdEntry)
    cmd.Command("config", "Manage Nginx vhost files.", hostConfigCmdEntry)
}

func hostAddCmdEntry(cmd *cli.Cmd) {
    cmd.Spec = "-d -c [-p]"
    domainName := cmd.StringOpt("d domain", "", "Domain name")
    containerName := cmd.StringOpt("c container", "", "Container name to forward requests to")
    containerPort := cmd.IntOpt("p port", 80, "Optional container port")
    cmd.Action = func() {
        newHost := Host{Domain: *domainName, ContainerName: *containerName, ContainerPort: *containerPort}
        newHost.Add()
    }
}

func hostDelCmdEntry(cmd *cli.Cmd) {
    cmd.Spec = "-d"
    domainName := cmd.StringOpt("d domain", "", "Domain name")
    cmd.Action = func() {
        oldHost := Host{Domain: *domainName}
        oldHost.Del()
    }
}

func hostLsCmdEntry(cmd *cli.Cmd) {
    cmd.Action = func() {
        names := getAllHostsNames()
        for _, name := range *names {
            fmt.Println(name)
        }
    }
}

func hostConfigCmdEntry(cmd *cli.Cmd) {
    cmd.Command("recreate", "Recreate nginx vhost file for one or multiple hosts", hostConfigRecreateCmdEntry)
}

func hostConfigRecreateCmdEntry(cmd *cli.Cmd) {
    cmd.Spec = "--all | HOSTS..."
    allFlag := cmd.BoolOpt("all", false, "Recreate all vhost configs")
    hostsList := cmd.StringsArg("HOSTS", nil, "List of host names to recreate vhost configs for")
    cmd.Action = func() {
        if *allFlag { hostsList = getAllHostsNames() }
        recreateVhostForHosts(hostsList)
    }
}

func (h *Host) Add() {
    exists := h.checkIfExists()
    if exists { log.Fatalln("Domain already exists") }

    h.appendVhostHTTP()
    restartNginx()

    h.issueAndInstallCert()

    h.appendVhostHTTPS()
    //h.saveToDB()

    restartNginx()
}

func (h *Host) Del() {
    exists := h.checkIfExists()
    if !exists {
        log.Fatalln("Domain does not exist")
    }
    h.removeVhost()
    h.removeCert()
}

func getDB() (*sql.DB, error) {
    return sql.Open("sqlite3", "/var/hostmanager/hosts.sqlite3")
}

func (h *Host) saveToDB() {
    var err error
    db, err := getDB()
    if err != nil {
        log.Println(err.Error())
        return
    }

    _, err = db.Query(
        "INSERT INTO hosts (domainname, containername, containerport) VALUES (?, ?, ?)",
        h.Domain, h.ContainerName, h.ContainerPort)
    if err != nil {
        log.Println("Failed to insert host into database")
        log.Println(err.Error())
        return
    }

    db.Close()
}

func loadHostFromDB(domainName string) (*Host, error) {
    var err error
    db, err := getDB()
    if err != nil { return nil, err }

    row := db.QueryRow("SELECT * FROM hosts WHERE domainname = ? LIMIT 1", domainName)

    h := Host{}
    err = row.Scan(&h)
    if err != nil { return nil, err }

    return &h, nil
}

func getAllHostsFromDB() (*[]Host, error) {
    var err error
    db, err := getDB()
    if err != nil { return nil, err }

    rows, err := db.Query("SELECT * FROM hosts")
    if err != nil { return nil, err }

    hosts := make([]Host, 0)
    for ; rows.Next() ; {
        h := Host{}
        err := rows.Scan(&h)
        if err != nil {
            log.Println(err.Error())
            continue
        }
        hosts = append(hosts, h)
    }
    return &hosts, nil
}

func (h *Host) checkIfExists() bool {
    // FIXME
    sslCertsDir := h.getSslCertsDir()
    _, err := os.Stat(sslCertsDir)
    exists := err == nil
    return exists
}

func (h *Host) issueAndInstallCert() {
    log.Printf("Issuing cert for domain %v with acme.sh...\n", h.Domain)
    h.ensureAcmeChallengeDirExists()

    var err error

    err = acmeSH("--issue", "-d", h.Domain, "-w", h.getWebroot())
    handleError(err, "Acme.sh could not issue a new cert")

    sslCertsDir := h.ensureSSLCertsDirExists()
    err = acmeSH(
        "--install-cert",
        "-d", h.Domain,
        "--cert-file", path.Join(sslCertsDir, "cert.pem"),
        "--key-file", path.Join(sslCertsDir, "key.pem"),
        "--fullchain-file", path.Join(sslCertsDir, "fullchain.pem"),
        "--reloadCmd", "manage restart nginx")
    handleError(err, "Acme.sh could not install cert. Exitting")
}

func (h *Host) removeVhost() {
    vhostFilePath := h.getVhostFilepath()
    err := os.Remove(vhostFilePath)
    if err != nil {
        log.Println("Failed to remove vhost file")
        log.Println(err.Error())
    }
    restartNginx()
}

func (h *Host) removeCert() {
    var err error

    err = acmeSH("--revoke", "-d", h.Domain)
    if err != nil {
        log.Println("Failed to revoke a cert")
        log.Println(err.Error())
    }

    err = acmeSH("--remove", "-d", h.Domain)
    if err != nil {
        log.Println("Failed to remove a cert")
        log.Println(err.Error())
    }

    acmeSslCertsDir := fmt.Sprintf("/root/.acme.sh/%s", h.Domain)
    removeAllIfExists(acmeSslCertsDir)

    sslCertsDir := h.getSslCertsDir()
    removeAllIfExists(sslCertsDir)
}

func (h *Host) ensureAcmeChallengeDirExists() string {
    acmeChallengeDir := path.Join(h.getWebroot(), ".well-known/acme-challenge")

    err := os.MkdirAll(acmeChallengeDir, 0777)
    handleError(err, "Could not create directory for acme challenge")

    return acmeChallengeDir
}

func (h *Host) ensureSSLCertsDirExists() string {
    sslCertsDir := h.getSslCertsDir()

    err := os.MkdirAll(sslCertsDir, 0600)
    handleError(err, "Could not create ssl cert directory")

    nginxUser, err := user.Lookup("nginx")
    handleError(err, "Could not find user `nginx`")

    uid, _ := strconv.Atoi(nginxUser.Uid)
    gid, _ := strconv.Atoi(nginxUser.Gid)

    err = os.Chown(sslCertsDir, uid, gid)
    handleError(err, "Could not chown to sslcerts dir")

    return sslCertsDir
}

func getAllHostsNames() *[]string {
    // FIXME
    files, err := ioutil.ReadDir("/etc/sslcerts")
    if err != nil {
        log.Println("Failed to get list of all hosts")
        log.Fatalln(err.Error())
    }

    hostNames := make([]string, 0)
    for _, f := range files {
        if f.IsDir() {
            hostNames = append(hostNames, f.Name())
        }
    }

    return &hostNames
}

func (h *Host) getWebroot() string {
    return fmt.Sprintf("/var/www/%s", h.Domain)
}

func (h *Host) getSslCertsDir() string {
    return fmt.Sprintf("/etc/sslcerts/%s", h.Domain)
}

func (h *Host) getVhostFilepath() string {
    return fmt.Sprintf("/etc/nginx/conf.d/%s.conf", h.Domain)
}

func recreateVhostForHosts(hostNames *[]string) {
    for _, name := range *hostNames {
        err := recreateVhost(name)
        if err != nil {
            log.Printf("Failed to recreate vhost for %s\n", name)
            log.Println(err.Error())
        }
    }
    restartNginx()
}

func recreateVhost(name string) error {
    var err error
    hst := Host{Domain: name}

    vhostFilePath := hst.getVhostFilepath()
    if _, err := os.Stat(vhostFilePath); err != nil { return err }

    err = hst.appendVhostHTTP()
    if err != nil { return err }

    err = hst.appendVhostHTTPS()
    return err
}

func (h *Host) appendVhostHTTP() error {
    tpl80, _ := h.getTemplates()

    confFile, err := os.OpenFile(h.getVhostFilepath(), os.O_TRUNC|os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0666)
    handleError(err, "Could not open vhost file")

    err = tpl80.Execute(confFile, h)
    handleError(err, "Could not execute template")

    confFile.Close()
    return err
}

func (h *Host) appendVhostHTTPS() error {
    _, tpl443 := h.getTemplates()

    confFile, err := os.OpenFile(h.getVhostFilepath(), os.O_APPEND|os.O_WRONLY, 0666)
    handleError(err, "Could not open vhost file")

    err = tpl443.Execute(confFile, h)
    handleError(err, "Could not execute template")

    confFile.Close()
    return err
}

func (h *Host) getTemplates() (*template.Template, *template.Template) {
    if h.tpl80 == nil || h.tpl443 == nil {
        h.loadTemplates()
    }
    return h.tpl80, h.tpl443
}

func (h *Host) loadTemplates() {
    tpl80s := `
    server {
        listen 80;
        server_name ((( .Domain )));

        location / {
            return 301 https://$host$request_uri;
        }

        location /.well-known/acme-challenge {
            alias /var/www/((( .Domain )))/.well-known/acme-challenge;
            try_files $uri $uri/;
        }
    }
    `

    tpl443s := `
    server {
        listen 443 ssl;
        server_name ((( .Domain )));

        if ($host != "((( .Domain )))") {
            return 403;
        }

        ssl_certificate /etc/sslcerts/((( .Domain )))/fullchain.pem;
        ssl_certificate_key /etc/sslcerts/((( .Domain )))/key.pem;

        error_log /var/log/nginx/((( .Domain ))).error.log;
        access_log /var/log/nginx/((( .Domain ))).log;

        keepalive_timeout 5;

        location @app {
            set $containerName ((( .ContainerName )));
            set $containerPort ((( .ContainerPort )));
            proxy_pass http://$containerName:$containerPort;
            proxy_set_header Host $host;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_redirect off;
        }

        location / {
            try_files @app @app;
        }
    }
    `

    tpl80 := template.New("tpl80")
    tpl80.Delims("(((", ")))")
    tpl443 := template.New("tpl443")
    tpl443.Delims("(((", ")))")

    tpl80.Parse(tpl80s)
    tpl443.Parse(tpl443s)

    h.tpl80 = tpl80
    h.tpl443 = tpl443
}

func restartNginx() {
    proc := exec.Command("manage", "restart", "nginx")
    proc.Stdout = os.Stdout
    proc.Stderr = os.Stderr
    proc.Start()
    proc.Wait()
}

func acmeSH(args ...string) error {
    proc := exec.Command("/root/.acme.sh/acme.sh", args...)
    proc.Stderr = os.Stderr
    proc.Stdout = os.Stdout

    var err error

    err = proc.Start()
    if err != nil {
        return err
    }

    err = proc.Wait()
    if err != nil {
        return err
    }

    return nil
}

func handleError(err error, msg string) {
    if err != nil {
        log.Println(msg)
        log.Fatalln(err)
    }
}

func removeAllIfExists(path string) {
    if _, err := os.Stat(path); err != nil {
        log.Printf("Directory %s does not exist. Skipping.\n", path)
    } else {
        err := os.RemoveAll(path)
        if err != nil {
            log.Printf("Failed to remove %s directory\n", path)
            log.Println(err.Error())
        }
    }
}
