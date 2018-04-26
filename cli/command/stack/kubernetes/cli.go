package kubernetes

import (
	"os"
	"path/filepath"

	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/kubernetes"
	"github.com/docker/docker/pkg/homedir"
	flag "github.com/spf13/pflag"
	kubeclient "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
)

// KubeCli holds kubernetes specifics (client, namespace) with the command.Cli
type KubeCli struct {
	command.Cli
	kubeConfig    *restclient.Config
	kubeNamespace string
	clientSet     *kubeclient.Clientset
}

// Options contains resolved parameters to initialize kubernetes clients
type Options struct {
	Namespace string
	Config    string
}

// NewOptions returns an Options initialized with command line flags
func NewOptions(flags *flag.FlagSet, namespace ...string) Options {
	var opts Options
	if len(namespace) > 0 {
		opts.Namespace = namespace[0]
	} else if nm, err := flags.GetString("namespace"); err == nil {
		opts.Namespace = nm
	}
	if kubeConfig, err := flags.GetString("kubeconfig"); err == nil {
		opts.Config = kubeConfig
	}
	return opts
}

// AddNamespaceFlag adds the namespace flag to the given flag set
func AddNamespaceFlag(flags *flag.FlagSet) {
	flags.String("namespace", "default", "Kubernetes namespace to use")
	flags.SetAnnotation("namespace", "kubernetes", nil)
	flags.SetAnnotation("namespace", "experimentalCLI", nil)
}

// WrapCli wraps command.Cli with kubernetes specifics
func WrapCli(dockerCli command.Cli, opts Options) (*KubeCli, error) {
	cli := &KubeCli{
		Cli: dockerCli,
	}
	kubeConfig := opts.Config
	if kubeConfig == "" {
		if config := os.Getenv("KUBECONFIG"); config != "" {
			kubeConfig = config
		} else {
			kubeConfig = filepath.Join(homedir.Get(), ".kube/config")
		}
	}

	clientConfig := kubernetes.NewKubernetesConfig(kubeConfig)

	configNamespace, _, err := clientConfig.Namespace()
	if err != nil {
		return nil, err
	}
	cli.kubeNamespace = configNamespace
	if opts.Namespace != "default" {
		cli.kubeNamespace = opts.Namespace
	}

	config, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, err
	}
	cli.kubeConfig = config

	clientSet, err := kubeclient.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	cli.clientSet = clientSet

	return cli, nil
}

func (c *KubeCli) composeClient() (*Factory, error) {
	return NewFactory(c.kubeNamespace, c.kubeConfig, c.clientSet)
}
