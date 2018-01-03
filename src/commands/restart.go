package commands

import (
	"log"
	"os/exec"
	"github.com/jawher/mow.cli"
	"fmt"
)

func RestartCmdEntry(cmd *cli.Cmd) {
	cmd.Spec = "SERVICES..."
	servicesList := cmd.StringsArg("SERVICES", nil, "Services to restart")
	cmd.Action = func() {
		restart(servicesList)
	}
}

func restart(servicesList *[]string) {
	for idx := range *servicesList {
		serviceName := (*servicesList)[idx]
		proc := exec.Command("immortalctl", "restart", serviceName)

		err := proc.Start()
		if err != nil {
			log.Println("Could not execute immortalctl")
			log.Println(err.Error())
			restartErrMsg(serviceName)
			continue
		}

		err = proc.Wait()
		if err != nil {
			log.Println("Failed to wait until restart process is done")
			log.Println(err.Error())
			restartErrMsg(serviceName)
			continue
		}

		fmt.Printf("Send restart command for %s. OK\n", serviceName)
	}
}

func restartErrMsg(serviceName string) {
	log.Printf("Failed to restart %s service\n", serviceName)
}

