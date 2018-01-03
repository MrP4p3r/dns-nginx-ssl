package commands

import (
	"os"
	"os/exec"
	"os/user"
	"fmt"
	"log"
	"path"
	"strconv"
	"text/template"
	"github.com/jawher/mow.cli"
)

type Host struct {
	Domain string
	ContainerName string
	ContainerPort int
	tpl80 *template.Template
	tpl443 *template.Template
	vhostConfig string
}

func HostCmdEntry(cmd *cli.Cmd) {
	cmd.Command("add", "Add Host and issue cert", hostAddCmdEntry)
}

func hostAddCmdEntry(cmd *cli.Cmd) {
	cmd.Spec = "-d -c [-p]"
	domainName := cmd.StringOpt("d domain", "", "Domain name of Host")
	containerName := cmd.StringOpt("c container", "", "Container name to forward requests to")
	containerPort := cmd.IntOpt("p port", 80, "Optional container port")
	cmd.Action = func() {
		newHost := Host{Domain: *domainName, ContainerName: *containerName, ContainerPort: *containerPort}
		newHost.Add()
	}
}

func (h *Host) Add() {
	h.appendVhostHTTP()
	h.issueAndInstallCert()
	h.appendVhostHTTPS()
}

func (h *Host) issueAndInstallCert() {
	fmt.Printf("Issuing cert for domain %v with acme.sh...\n", h.Domain)
	h.ensureAcmeChallengeDirExists()

	proc := exec.Command(
		"/root/.acme.sh/.acme.sh", "--issue",
		"-d", h.Domain, "-w", h.getWebroot())
	proc.Stderr = os.Stderr
	proc.Stdout = os.Stdout

	err := proc.Start()
	handleError(err, "Failed to start acme.sh")

	err = proc.Wait()
	handleError(err, "Acme.sh could not issue a new cert")

	sslCertsDir := h.ensureSSLCertsDirExists()
	proc = exec.Command(
		"/root/.acme.sh/acme.sh", "--install-cert",
		"-d", h.Domain,
		"--cert-file", path.Join(sslCertsDir, "cert.pem"),
		"--key-file", path.Join(sslCertsDir, "key.pem"),
		"--reloadCmd", "manage restart nginx")
	proc.Stdout = os.Stdout
	proc.Stderr = os.Stderr
	proc.Start()

	err = proc.Wait()
	handleError(err, "Acme.sh could not install cert. Exitting")
}

func (h *Host) ensureAcmeChallengeDirExists() string {
	acmeChallengeDir := path.Join(h.getWebroot(), ".well-known/acme-challenge")

	err := os.MkdirAll(acmeChallengeDir, 0777)
	handleError(err, "Could not create directory for acme challenge")

	return acmeChallengeDir
}

func (h *Host) ensureSSLCertsDirExists() string {
	sslCertsDir := fmt.Sprintf("/etc/sslcerts/%s", h.Domain)

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
	webRoot := fmt.Sprintf("/var/www/%s", h.Domain)
	return webRoot
}

func (h *Host) appendVhostHTTP() {
	tpl80, _ := h.getTemplates()

	confFile, err := os.OpenFile(h.getVhostFilepath(), os.O_TRUNC | os.O_CREATE | os.O_APPEND | os.O_WRONLY, 0666)
	handleError(err, "Could not open vhost file")

	err = tpl80.Execute(confFile, h)
	handleError(err, "Could not execute template")

	confFile.Close()
}

func (h *Host) appendVhostHTTPS() {
	_, tpl443 := h.getTemplates()

	confFile, err := os.OpenFile(h.getVhostFilepath(), os.O_APPEND | os.O_WRONLY, 0666)
	handleError(err, "Could not open vhost file")

	err = tpl443.Execute(confFile, h)
	handleError(err, "Could not execute template")

	confFile.Close()
}

func (h *Host) getVhostFilepath() string {
	return fmt.Sprintf("/etc/nginx/conf.d/%s.conf", h.Domain)
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

		ssl_certificate /etc/sslcerts/((( .Domain )))/cert.pem;
		ssl_certificate_key /etc/sslcerts/((( .Domain )))/key.pem;

		error_log /var/log/nginx/((( .Domain ))).error.log;
		access_log /var/log/nginx/((( .Domain ))).log;

		keepalive_timeout 5;

		location @app {
			proxy_pass http://((( .ContainerName ))):((( .ContainerPort )));
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

func handleError(err error, msg string) {
	if err != nil {
		log.Println(msg)
		log.Fatalln(err)
	}
}