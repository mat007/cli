package stack

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"sort"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/formatter"
	"github.com/docker/cli/cli/command/stack/kubernetes"
	"github.com/docker/cli/cli/command/stack/options"
	"github.com/docker/cli/cli/command/stack/swarm"
	"github.com/docker/go-connections/tlsconfig"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	core_v1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"vbom.ml/util/sortorder"
)

func newListCommand(dockerCli command.Cli) *cobra.Command {
	opts := options.List{}

	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List stacks",
		Args:    cli.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd, dockerCli, opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Format, "format", "", "Pretty-print stacks using a Go template")
	flags.StringSliceVar(&opts.Namespaces, "namespace", []string{"default"}, "Kubernetes namespaces to use")
	flags.SetAnnotation("namespace", "kubernetes", nil)
	flags.SetAnnotation("namespace", "experimentalCLI", nil)
	flags.BoolVarP(&opts.AllNamespaces, "all-namespaces", "", false, "List stacks among all Kubernetes namespaces")
	flags.SetAnnotation("all-namespaces", "kubernetes", nil)
	flags.SetAnnotation("all-namespaces", "experimentalCLI", nil)
	return cmd
}

func runList(cmd *cobra.Command, dockerCli command.Cli, opts options.List) error {
	stacks := []*formatter.Stack{}
	if dockerCli.ClientInfo().HasSwarm() {
		ss, err := swarm.GetStacks(dockerCli)
		if err != nil {
			return err
		}
		stacks = append(stacks, ss...)
	}
	if dockerCli.ClientInfo().HasKubernetes() {
		flags := cmd.Flags()
		if dockerCli.ClientInfo().HasAll() && !flags.Changed("namespace") {
			opts.AllNamespaces = true
		}
		if opts.AllNamespaces {
			ss, err := getStacksWithAllNamespaces(dockerCli, opts, flags)
			if err != nil {
				return err
			}
			stacks = append(stacks, ss...)
		} else {
			ss, err := getStacksWithNamespaces(dockerCli, opts, flags)
			if err != nil {
				return err
			}
			stacks = append(stacks, ss...)
		}
	}
	return format(dockerCli, opts, stacks)
}

func getStacksWithAllNamespaces(dockerCli command.Cli, opts options.List, flags *pflag.FlagSet) ([]*formatter.Stack, error) {
	stacks, err := getStacks(dockerCli, opts, kubernetes.NewOptions(flags))
	if err == nil || !apierrs.IsForbidden(err) {
		return stacks, err
	}
	nms, err := getUserVisibleNamespaces(dockerCli, err)
	if err != nil {
		return nil, err
	}
	opts.AllNamespaces = false
	for _, nm := range nms.Items {
		ss, err := getStacks(dockerCli, opts, kubernetes.NewOptions(flags, nm.Name))
		if err != nil {
			return nil, err
		}
		stacks = append(stacks, ss...)
	}
	return stacks, nil
}

func getStacksWithNamespaces(dockerCli command.Cli, opts options.List, flags *pflag.FlagSet) ([]*formatter.Stack, error) {
	mnms, err := getNamespaces(flags)
	if err != nil {
		return nil, err
	}
	stacks := []*formatter.Stack{}
	for nm := range mnms {
		ss, err := getStacks(dockerCli, opts, kubernetes.NewOptions(flags, nm))
		if err != nil {
			return nil, err
		}
		stacks = append(stacks, ss...)
	}
	return stacks, nil
}

func getStacks(dockerCli command.Cli, opts options.List, kopts kubernetes.Options) ([]*formatter.Stack, error) {
	kli, err := kubernetes.WrapCli(dockerCli, kopts)
	if err != nil {
		return nil, err
	}
	ss, err := kubernetes.GetStacks(kli, opts)
	if err != nil {
		return nil, err
	}
	return ss, nil
}

func getNamespaces(flags *pflag.FlagSet) (map[string]struct{}, error) {
	nms, err := flags.GetStringSlice("namespace")
	if err != nil {
		return nil, err
	}
	mnms := map[string]struct{}{}
	for _, nm := range nms {
		mnms[nm] = struct{}{}
	}
	return mnms, nil
}

func getUserVisibleNamespaces(dockerCli command.Cli, forbiddenErr error) (*core_v1.NamespaceList, error) {
	host := dockerCli.Client().DaemonHost()
	endpoint, err := url.Parse(host)
	if err != nil || endpoint.Scheme != "tcp" {
		return nil, forbiddenErr
	}
	tlsOptions := dockerCli.ClientInfo().TLSOptions
	if tlsOptions == nil {
		return nil, forbiddenErr
	}
	tlsConfig, err := tlsconfig.Client(*tlsOptions)
	if err != nil {
		return nil, err
	}
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}
	endpoint.Scheme = "https"
	endpoint.Path = "/kubernetesNamespaces"
	return listUserNamespaces(httpClient, *endpoint)
}

func listUserNamespaces(httpClient *http.Client, endpoint url.URL) (*core_v1.NamespaceList, error) {
	resp, err := httpClient.Get(endpoint.String())
	if err != nil {
		return nil, errors.Wrap(err, "unable to get user namespaces")
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to list user namespaces: received %d status and unable to read response", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unable to list user namespaces: %s", string(body))
	}
	var res core_v1.NamespaceList
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, errors.Wrapf(err, "unable to unmarshal user namespaces: %s", string(body))
	}
	return &res, nil
}

func format(dockerCli command.Cli, opts options.List, stacks []*formatter.Stack) error {
	var format string
	switch {
	case opts.Format != "":
		format = opts.Format
	case dockerCli.ClientInfo().HasKubernetes():
		format = formatter.KubernetesStackTableFormat
	default:
		format = formatter.SwarmStackTableFormat
	}
	stackCtx := formatter.Context{
		Output: dockerCli.Out(),
		Format: formatter.NewStackFormat(format),
	}
	sort.Slice(stacks, func(i, j int) bool {
		return sortorder.NaturalLess(stacks[i].Name, stacks[j].Name)
	})
	return formatter.StackWrite(stackCtx, stacks)
}
