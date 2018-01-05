package commands

import (
	"github.com/jawher/mow.cli"
	"log"
	"os/exec"
)

func RestartCmdEntry(cmd *cli.Cmd) {
	cmd.Spec = "SERVICES..."
	servicesList := cmd.StringsArg("SERVICES", nil, "Services to restart")
	cmd.Action = func() {
		restartMany(servicesList)
	}
}

func restartMany(servicesList *[]string) {
	for _, serviceName := range *servicesList {
		err := restart(serviceName)
		if err == nil {
			log.Printf("Send restart command for %s. OK\n", serviceName)
		}
	}
}

func restart(serviceName string) error {
	proc := exec.Command("immortalctl", "restart", serviceName)

	err := proc.Start()
	if err != nil {
		log.Println("Could not execute immortalctl")
		log.Println(err.Error())
		restartErrMsg(serviceName)
		return err
	}

	err = proc.Wait()
	if err != nil {
		log.Println("Failed to wait until restart process is done")
		log.Println(err.Error())
		restartErrMsg(serviceName)
		return err
	}

	return nil
}

func restartErrMsg(serviceName string) {
	log.Printf("Failed to restart %s service\n", serviceName)
}
