package service

import (
	"fmt"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type SenHubService struct {
	Name        string
	AgentPath   string
	ExecutePath string
	elog        *eventlog.Log
	isDebug     bool
}

func New(name, dir string) (*SenHubService, error) {
	agentPath := filepath.Join(dir, "senhub-agent_windows_amd64.exe")
	exePath, err := os.Executable()
	if err != nil {
		return nil, err
	}

	s := &SenHubService{
		Name:        name,
		AgentPath:   agentPath,
		ExecutePath: exePath,
		isDebug:     false,
	}

	if !s.isDebug {
		elog, err := eventlog.Open(name)
		if err == nil {
			s.elog = elog
		}
	}

	return s, nil
}

func (s *SenHubService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown | svc.AcceptPauseAndContinue

	changes <- svc.Status{State: svc.StartPending}

	// Récupérer la clé d'authentification depuis les variables d'environnement
	authKey := os.Getenv("SENHUB_KEY")
	if authKey == "" {
		if s.elog != nil {
			s.elog.Error(1, "SENHUB_KEY environment variable is not set")
		}
		return true, 1
	}

	// Créer la commande avec les paramètres
	cmd := exec.Command(s.AgentPath, "--authentication-key", authKey)
	err := cmd.Start()
	if err != nil {
		if s.elog != nil {
			s.elog.Error(1, fmt.Sprintf("Failed to start agent: %v", err))
		}
		return true, 1
	}

	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				if cmd.Process != nil {
					cmd.Process.Signal(syscall.SIGTERM)
					time.Sleep(time.Second) // Attendre la fermeture propre
					cmd.Process.Kill()      // Force kill si nécessaire
				}
				return false, 0
			case svc.Pause:
				changes <- svc.Status{State: svc.Paused, Accepts: cmdsAccepted}
			case svc.Continue:
				changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
			}
		}
	}
}

func (s *SenHubService) Install() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	service, err := m.OpenService(s.Name)
	if err == nil {
		service.Close()
		return fmt.Errorf("service %s already exists", s.Name)
	}

	service, err = m.CreateService(
		s.Name,
		s.ExecutePath,
		mgr.Config{
			DisplayName: "SenHub Agent Service",
			Description: "Service for SenHub Agent",
			StartType:   mgr.StartAutomatic,
		},
	)
	if err != nil {
		return err
	}
	defer service.Close()

	err = eventlog.InstallAsEventCreate(s.Name, eventlog.Error|eventlog.Warning|eventlog.Info)
	if err != nil {
		service.Delete()
		return fmt.Errorf("SetupEventLogSource() failed: %s", err)
	}

	return nil
}

func (s *SenHubService) Uninstall() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	service, err := m.OpenService(s.Name)
	if err != nil {
		return fmt.Errorf("service %s not found", s.Name)
	}
	defer service.Close()

	err = service.Delete()
	if err != nil {
		return err
	}

	err = eventlog.Remove(s.Name)
	if err != nil {
		return fmt.Errorf("RemoveEventLogSource() failed: %s", err)
	}

	return nil
}

func (s *SenHubService) Start() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	service, err := m.OpenService(s.Name)
	if err != nil {
		return fmt.Errorf("could not access service: %v", err)
	}
	defer service.Close()

	err = service.Start()
	if err != nil {
		return fmt.Errorf("could not start service: %v", err)
	}

	return nil
}

func (s *SenHubService) Stop() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	service, err := m.OpenService(s.Name)
	if err != nil {
		return fmt.Errorf("could not access service: %v", err)
	}
	defer service.Close()

	status, err := service.Control(svc.Stop)
	if err != nil {
		return fmt.Errorf("could not send stop control: %v", err)
	}

	timeout := time.Now().Add(10 * time.Second)
	for status.State != svc.Stopped {
		if timeout.Before(time.Now()) {
			return fmt.Errorf("timeout waiting for service to stop")
		}
		time.Sleep(300 * time.Millisecond)
		status, err = service.Query()
		if err != nil {
			return fmt.Errorf("could not retrieve service status: %v", err)
		}
	}

	return nil
}

func (s *SenHubService) Run() error {
	isIntSess, err := svc.IsAnInteractiveSession()
	if err != nil {
		return err
	}

	if isIntSess {
		return debug.Run(s.Name, s)
	}
	return svc.Run(s.Name, s)
}

func Control(s *SenHubService, command string) error {
	cmd := strings.ToLower(command)

	switch cmd {
	case "install":
		return s.Install()
	case "uninstall":
		return s.Uninstall()
	case "start":
		return s.Start()
	case "stop":
		return s.Stop()
	default:
		return fmt.Errorf("invalid command: %s", command)
	}
}
