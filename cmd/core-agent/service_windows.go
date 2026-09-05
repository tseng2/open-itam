//go:build windows

package main

import (
	"log"

	"golang.org/x/sys/windows/svc"
)

const serviceName = "ITAgentService"

type agentService struct {
	cfgPath string
}

func tryRunAsService(cfgPath string, force bool) bool {
	isSvc, err := svc.IsWindowsService()
	if err != nil || (!isSvc && !force) {
		return false
	}
	if err := svc.Run(serviceName, &agentService{cfgPath: cfgPath}); err != nil {
		log.Printf("service run: %v", err)
	}
	return true
}

func (s *agentService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	changes <- svc.Status{State: svc.StartPending}
	done := make(chan struct{})
	go func() {
		innerMain(s.cfgPath, done)
	}()
	changes <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for c := range r {
		switch c.Cmd {
		case svc.Interrogate:
			changes <- c.CurrentStatus
		case svc.Stop, svc.Shutdown:
			changes <- svc.Status{State: svc.StopPending}
			close(done)
			return
		}
	}
	return
}
