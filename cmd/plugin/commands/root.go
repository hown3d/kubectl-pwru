package commands

import (
	"os"
	"strings"

	"github.com/hown3d/kubectl-pwru/cmd/plugin/commands/completion"
	"github.com/hown3d/kubectl-pwru/pkg/plugin"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"k8s.io/cli-runtime/pkg/genericiooptions"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	utilcomp "k8s.io/kubectl/pkg/util/completion"
)

type Options struct {
	genericiooptions.IOStreams

	ConfigFlags *genericclioptions.ConfigFlags
}

var (
	config = &plugin.Config{}
)

func NewOptions() *Options {
	ioStreams := genericiooptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}
	return &Options{
		IOStreams:   ioStreams,
		ConfigFlags: genericclioptions.NewConfigFlags(true).WithDeprecatedPasswordFlag().WithDiscoveryBurst(300).WithDiscoveryQPS(50.0).WithWarningPrinter(ioStreams),
	}
}

func RootCmd() *cobra.Command {
	o := NewOptions()
	f := cmdutil.NewFactory(o.ConfigFlags)
	cmd := &cobra.Command{
		Use:          "kubectl-pwru",
		SilenceUsage: true,
		Args:         cobra.MinimumNArgs(1),
		PreRun: func(cmd *cobra.Command, args []string) {
			viper.BindPFlags(cmd.Flags())
		},

		ValidArgsFunction: validArgsFunc(f),
		RunE: func(cmd *cobra.Command, args []string) error {
			plug, err := plugin.New(o.ConfigFlags, config)
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

	o.ConfigFlags.AddFlags(cmd.Flags())
	config.AddFlags(cmd.Flags())

	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	for _, subcommand := range []*cobra.Command{
		completion.NewCommand(o.IOStreams),
	} {
		cmd.AddCommand(subcommand)
	}
	return cmd
}

func validArgsFunc(f cmdutil.Factory) cobra.CompletionFunc {
	firstArgTypes := []string{"pods"}
	secondArgTypes := []string{"pods", "services"}
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		var validTypes []string
		if len(args) < 1 {
			validTypes = firstArgTypes
		}
		if len(args) >= 1 {
			validTypes = secondArgTypes
		}
		return utilcomp.SpecifiedResourceTypeAndNameCompletionFunc(f, validTypes)(cmd, args, toComplete)
	}
}
