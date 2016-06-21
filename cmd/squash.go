package cmd

import (
	"io"
	"os"

	"golang.org/x/net/context"

	"github.com/spf13/cobra"
)

// squashCmd represents the squash command
var squashCmd = &cobra.Command{
	Use:   "squash [input] [output]",
	Short: "Flatten a Docker image",
	Long: `Flatten a Docker image to its final layer and output the image as a
tar archive suitable for Docker import.

Specify "-" for input or output to use stdin or stdout, respectively.`,
	Run: squash,
}

func init() {
	RootCmd.AddCommand(squashCmd)
}

func squash(cmd *cobra.Command, args []string) {
	var input io.ReadCloser
	var output io.WriteCloser
	var err error
	if len(args) != 2 {
		clierr("input and output required")
	}
	switch args[0] {
	case "-":
		input = os.Stdin
	default:
		input, err = os.Open(args[0])
		defer input.Close()
		if err != nil {
			clierr("error opening input: %v", err)
		}
	}
	switch args[1] {
	case "-":
		output = os.Stdout
	default:
		output, err = os.Open(args[0])
		defer output.Close()
		if err != nil {
			clierr("error opening input: %v", err)
		}
	}

	squasher := DockerImageSquasher{}
	sqi, err := squasher.Squash(context.Background(), input)
	if err != nil {
		clierr("error squashing: %v", err)
	}
	_, err = io.Copy(output, sqi)
	if err != nil {
		clierr("error writing output: %v", err)
	}
}
