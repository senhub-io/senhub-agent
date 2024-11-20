package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"senhub-agent.go/internal/service"
)

var (
	svcName = "SenHubAgent"
)

func usage(errmsg string) {
	fmt.Fprintf(os.Stderr,
		"%s\n\n"+
			"usage: %s <command>\n"+
			"       où <command> est l'une des commandes suivantes:\n"+
			"       install, uninstall, start, stop, run\n",
		errmsg, os.Args[0])
	os.Exit(2)
}

func main() {
	flag.Parse()

	if len(os.Args) < 2 {
		usage("Aucune commande spécifiée")
		return
	}

	cmd := strings.ToLower(os.Args[1])

	dir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		log.Fatal(err)
	}

	svc, err := service.New(svcName, dir)
	if err != nil {
		log.Fatal(err)
	}

	switch cmd {
	case "install":
		err = svc.Install()
	case "uninstall":
		err = svc.Uninstall()
	case "start":
		err = svc.Start()
	case "stop":
		err = svc.Stop()
	case "run":
		err = svc.Run()
	default:
		usage(fmt.Sprintf("Commande invalide %s", cmd))
	}

	if err != nil {
		log.Fatalf("Action %s a échoué: %v", cmd, err)
	}
}
