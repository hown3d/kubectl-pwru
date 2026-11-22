package commands

import (
	"strings"

	"github.com/hown3d/kubectl-pwru/pkg/plugin"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

var (
	kubernetesConfigFlags *genericclioptions.ConfigFlags
	config                = &plugin.Config{}
)

func RootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "kubectl-pwru",
		SilenceUsage: true,
		Args:         cobra.MinimumNArgs(1),
		PreRun: func(cmd *cobra.Command, args []string) {
			viper.BindPFlags(cmd.Flags())
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			plug, err := plugin.New(kubernetesConfigFlags, config)
			if err != nil {
				return err
			}
			var target string
			if len(args) == 2 {
				target = args[1]
			}
			return plug.Run(cmd.Context(), args[0], target)
		},
	}

	cobra.OnInitialize(func() {
		viper.AutomaticEnv()
	})

	kubernetesConfigFlags = genericclioptions.NewConfigFlags(false)
	kubernetesConfigFlags.AddFlags(cmd.Flags())
	config.AddFlags(cmd.Flags())

	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	return cmd
}
