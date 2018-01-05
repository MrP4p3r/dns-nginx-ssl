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
		proc := exec.Command("ls", "-1", "/etc/sslcerts/")
		proc.Stdout = os.Stdout
		proc.Stderr = os.Stderr
		proc.Start()
		proc.Wait()
	}
}

func (h *Host) Add() {
	exists := h.checkIfExists()
	if exists {
		log.Fatalln("Domain already exists")
	}
	h.appendVhostHTTP()
	h.issueAndInstallCert()
	h.appendVhostHTTPS()
}

func (h *Host) Del() {
	exists := h.checkIfExists()
	if !exists {
		log.Fatalln("Domain does not exist")
	}
	h.removeVhost()
	h.removeCert()
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

func (h *Host) getWebroot() string {
	return fmt.Sprintf("/var/www/%s", h.Domain)
}

func (h *Host) getSslCertsDir() string {
	return fmt.Sprintf("/etc/sslcerts/%s", h.Domain)
}

func (h *Host) getVhostFilepath() string {
	return fmt.Sprintf("/etc/nginx/conf.d/%s.conf", h.Domain)
}

func (h *Host) appendVhostHTTP() {
	tpl80, _ := h.getTemplates()

	confFile, err := os.OpenFile(h.getVhostFilepath(), os.O_TRUNC|os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0666)
	handleError(err, "Could not open vhost file")

	err = tpl80.Execute(confFile, h)
	handleError(err, "Could not execute template")

	confFile.Close()
	restartNginx()
}

func (h *Host) appendVhostHTTPS() {
	_, tpl443 := h.getTemplates()

	confFile, err := os.OpenFile(h.getVhostFilepath(), os.O_APPEND|os.O_WRONLY, 0666)
	handleError(err, "Could not open vhost file")

	err = tpl443.Execute(confFile, h)
	handleError(err, "Could not execute template")

	confFile.Close()
	restartNginx()
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
