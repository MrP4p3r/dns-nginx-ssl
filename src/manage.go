package main

import (
    "os"
    "github.com/jawher/mow.cli"
    "./commands"
)

func main() {
    app := cli.App("manage", "manage some things inside container")

    app.Command("host", "Manage hosts", commands.HostCmdEntry)
    app.Command("restart", "Restart services", commands.RestartCmdEntry)

    app.Run(os.Args)
}
