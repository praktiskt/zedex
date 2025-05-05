package cmd

import (
	"github.com/sirupsen/logrus"
)

var baseFlags struct {
	debug bool
}

func manageDefaultFlags() {
	if baseFlags.debug {
		logrus.SetReportCaller(true)
		logrus.SetLevel(logrus.DebugLevel)
	}
}
