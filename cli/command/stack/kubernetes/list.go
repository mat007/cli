package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"

	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/formatter"
	"github.com/docker/cli/cli/command/stack/options"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	core_v1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetStacks lists the kubernetes stacks
func GetStacks(dockerCli command.Cli, opts options.List, flags *pflag.FlagSet) ([]*formatter.Stack, error) {
	if dockerCli.ClientInfo().HasAll() && !flags.Changed("namespace") {
		opts.AllNamespaces = true
	}
	if opts.AllNamespaces {
		return getStacksWithAllNamespaces(dockerCli, opts, flags)
	}
	return getStacksWithNamespaces(dockerCli, opts, flags)
}

func getStacksWithAllNamespaces(dockerCli command.Cli, opts options.List, flags *pflag.FlagSet) ([]*formatter.Stack, error) {
	stacks, err := getStacks(dockerCli, opts, NewOptions(flags))
	if err == nil || !apierrs.IsForbidden(err) {
		return stacks, err
	}
	nms, err2 := getUserVisibleNamespaces(dockerCli)
	if err2 != nil {
		logrus.Warnf("Failed to query user visible namespaces: %s", err2)
		return nil, err
	}
	opts.AllNamespaces = false
	for _, nm := range nms.Items {
		ss, err := getStacks(dockerCli, opts, NewOptions(flags, nm.Name))
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
		ss, err := getStacks(dockerCli, opts, NewOptions(flags, nm))
		if err != nil {
			return nil, err
		}
		stacks = append(stacks, ss...)
	}
	return stacks, nil
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

func getStacks(dockerCli command.Cli, opts options.List, kopts Options) ([]*formatter.Stack, error) {
	kubeCli, err := WrapCli(dockerCli, kopts)
	if err != nil {
		return nil, err
	}
	composeClient, err := kubeCli.composeClient()
	if err != nil {
		return nil, err
	}
	stackSvc, err := composeClient.Stacks(opts.AllNamespaces)
	if err != nil {
		return nil, err
	}
	stacks, err := stackSvc.List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var formattedStacks []*formatter.Stack
	for _, stack := range stacks {
		formattedStacks = append(formattedStacks, &formatter.Stack{
			Name:         stack.name,
			Services:     len(stack.getServices()),
			Orchestrator: "Kubernetes",
			Namespace:    stack.namespace,
		})
	}
	return formattedStacks, nil
}

func getUserVisibleNamespaces(dockerCli command.Cli) (*core_v1.NamespaceList, error) {
	host := dockerCli.Client().DaemonHost()
	endpoint, err := url.Parse(host)
	if err != nil {
		return nil, err
	}
	res := core_v1.NamespaceList{}
	err = dockerCli.Client().CustomRequest(context.Background(), "GET", "https", endpoint.Host, "/kubernetesNamespaces", nil, nil, nil, func(statusCode int, body io.Reader) error {
		bytes, err := ioutil.ReadAll(body)
		if err != nil {
			return errors.Wrapf(err, "received %d status and unable to read response", statusCode)
		}
		if statusCode != http.StatusOK {
			return fmt.Errorf(string(bytes))
		}
		if err := json.Unmarshal(bytes, &res); err != nil {
			return errors.Wrapf(err, "unmarshal failed: %s", string(bytes))
		}
		return nil
	})
	return &res, err
}
