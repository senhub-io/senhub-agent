package service

import (
	"fmt"
	"github.com/kardianos/service"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type SenHubService struct {
	service.Service
	agentPath string
	agentCmd  *exec.Cmd
	logger    service.Logger
	exit      chan struct{}
}

// Création d'une nouvelle instance du service
func New(name, dir string) (*SenHubService, error) {
	agentPath := filepath.Join(dir, "senhub-agent_windows_amd64.exe")
	svcConfig := &service.Config{
		Name:        name,
		DisplayName: "SenHub Agent Service",
		Description: "Service for SenHub Agent",
	}

	s := &SenHubService{
		agentPath: agentPath,
		exit:      make(chan struct{}),
	}

	svc, err := service.New(s, svcConfig)
	if err != nil {
		return nil, fmt.Errorf("cannot create service: %v", err)
	}

	s.Service = svc
	s.logger, err = svc.Logger(nil)
	if err != nil {
		return nil, fmt.Errorf("cannot create logger: %v", err)
	}

	return s, nil
}

// Start implémente l'interface service.Interface
func (s *SenHubService) Start(svc service.Service) error {
	s.logger.Info("Starting service...")

	// Vérification de l'existence de l'agent
	if _, err := os.Stat(s.agentPath); os.IsNotExist(err) {
		return fmt.Errorf("agent executable not found at %s", s.agentPath)
	}

	// Démarrage de l'agent en arrière-plan
	go s.run()

	return nil
}

// Stop implémente l'interface service.Interface
func (s *SenHubService) Stop(svc service.Service) error {
	s.logger.Info("Stopping service...")
	close(s.exit)

	if s.agentCmd != nil && s.agentCmd.Process != nil {
		s.logger.Info("Killing agent process...")
		if err := s.agentCmd.Process.Kill(); err != nil {
			s.logger.Errorf("Failed to kill process: %v", err)
		}
	}

	// Attendre que le processus se termine
	time.Sleep(time.Second)
	return nil
}

// run est la fonction principale qui exécute l'agent
func (s *SenHubService) run() {
	s.logger.Info("Starting agent process...")

	for {
		select {
		case <-s.exit:
			return
		default:
			// Exécution de l'agent avec seulement l'option "start"
			s.agentCmd = exec.Command(s.agentPath, "start")
			s.agentCmd.Dir = filepath.Dir(s.agentPath)

			// Configuration des logs
			logPath := filepath.Join(filepath.Dir(s.agentPath), "senhubagent-service.log")
			logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0666)
			if err == nil {
				s.agentCmd.Stdout = logFile
				s.agentCmd.Stderr = logFile
				defer logFile.Close()
			} else {
				s.logger.Errorf("Failed to create log file: %v", err)
			}

			// Démarrage du processus
			if err := s.agentCmd.Start(); err != nil {
				s.logger.Errorf("Failed to start agent: %v", err)
				time.Sleep(10 * time.Second) // Attendre avant de réessayer
				continue
			}

			s.logger.Info("Agent process started successfully")

			// Attendre que le processus se termine
			if err := s.agentCmd.Wait(); err != nil {
				s.logger.Errorf("Agent process exited with error: %v", err)
			}

			// Vérifier si on doit quitter
			select {
			case <-s.exit:
				return
			default:
				time.Sleep(5 * time.Second) // Attendre avant de redémarrer
			}
		}
	}
}

// Install installe le service
func (s *SenHubService) Install() error {
	return s.Service.Install()
}

// Uninstall désinstalle le service
func (s *SenHubService) Uninstall() error {
	return s.Service.Uninstall()
}

// Start démarre le service
func (s *SenHubService) StartService() error {
	return s.Service.Start()
}

// Stop arrête le service
func (s *SenHubService) StopService() error {
	return s.Service.Stop()
}

// Control gère les commandes du service
func Control(s *SenHubService, command string) error {
	switch command {
	case "install":
		return s.Install()
	case "uninstall":
		return s.Uninstall()
	case "start":
		return s.StartService()
	case "stop":
		return s.StopService()
	case "run":
		return s.Run()
	default:
		return fmt.Errorf("invalid command: %s", command)
	}
}
