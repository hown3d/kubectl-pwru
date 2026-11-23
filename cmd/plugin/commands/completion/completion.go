package completion

import (
	_ "embed"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

//go:embed kubectl_complete-pwru
var completionScript []byte

type Options struct {
	genericiooptions.IOStreams
}

func NewOptions(streams genericiooptions.IOStreams) *Options {
	return &Options{
		IOStreams: streams,
	}
}

func NewCommand(streams genericiooptions.IOStreams) *cobra.Command {
	o := NewOptions(streams)

	cmd := &cobra.Command{
		Use:                   "completion",
		DisableFlagsInUseLine: true,

		Short: "Setup shell completion",

		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Run())
		},
	}

	return cmd
}

// Run outputs the completion script.
func (o *Options) Run() error {
	_, err := o.Out.Write(completionScript)
	return err
}
