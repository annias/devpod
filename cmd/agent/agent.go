package agent

import (
	"github.com/loft-sh/devpod/cmd/agent/container"
	"github.com/loft-sh/devpod/cmd/agent/workspace"
	"github.com/loft-sh/devpod/cmd/flags"
	"github.com/loft-sh/devpod/pkg/log"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// NewAgentCmd returns a new root command
func NewAgentCmd(flags *flags.GlobalFlags) *cobra.Command {
	agentCmd := &cobra.Command{
		Use:   "agent",
		Short: "DevPod Agent",
		PersistentPreRunE: func(cobraCmd *cobra.Command, args []string) error {
			if flags.Silent {
				log.Default.SetLevel(logrus.FatalLevel)
			} else if flags.Debug {
				log.Default.SetLevel(logrus.DebugLevel)
			}

			log.Default.MakeRaw()
			return nil
		},
		Hidden: true,
	}

	agentCmd.AddCommand(workspace.NewWorkspaceCmd(flags))
	agentCmd.AddCommand(container.NewContainerCmd(flags))
	agentCmd.AddCommand(NewDaemonCmd(flags))
	agentCmd.AddCommand(NewContainerTunnelCmd(flags))
	agentCmd.AddCommand(NewGitCredentialsCmd(flags))
	agentCmd.AddCommand(NewDockerCredentialsCmd(flags))
	return agentCmd
}
