//go:build windows
// +build windows

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"senhub-agent.go/internal/service"
)

func main() {
	// Obtenir le répertoire courant
	dir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		log.Fatal(err)
	}

	// Créer le service
	svc, err := service.New("SenHubService", dir)
	if err != nil {
		log.Fatal(err)
	}

	// Si aucun argument n'est fourni, exécuter le service
	if len(os.Args) < 2 {
		if err := svc.Run(); err != nil {
			log.Fatal(err)
		}
		return
	}

	// Sinon, exécuter la commande demandée
	if err := service.Control(svc, os.Args[1]); err != nil {
		log.Fatal(err)
		fmt.Println("Usage: senhubagent-service.exe [install|uninstall|start|stop|run]")
	}
}
