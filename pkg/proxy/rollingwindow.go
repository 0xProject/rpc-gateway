package proxy

import "github.com/0xProject/rpc-gateway/pkg/rollingwindow"

type RollingWindowWrapper struct {
	rollingWindow *rollingwindow.RollingWindow
	Name          string
}

func NewRollingWindowWrapper(name string, size int) *RollingWindowWrapper {
	return &RollingWindowWrapper{
		Name:          name,
		rollingWindow: rollingwindow.NewRollingWindow(size),
	}
}
