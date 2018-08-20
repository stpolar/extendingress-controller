package main

import (
	"github.com/spf13/pflag"
	"flag"
	"os"
	"github.com/golang/glog"
	"fmt"
	ing_net "k8s.io/extendingress-controller/net"
)

func parseFlags() (bool, *Configuration, error) {
	var (
		flags= pflag.NewFlagSet("", pflag.ExitOnError)

		apiserverHost = flags.String("apiserver-host", "",
			`Address of the Kubernetes API server.
Takes the form "protocol://address:port". If not specified, it is assumed the
program runs inside a Kubernetes cluster and local discovery is attempted.`)

		commonConf = flag.String("commonConf", "",
			"Name of the ConfigMap containing custom common global configurations for the controller.")

		eventConf = flag.String("eventConf", "",
			"Name of the ConfigMap containing custom event global configurations for the controller.")

		httpConf = flag.String("httpConf", "",
			"Name of the ConfigMap containing custom http global configurations for the controller.")

		kubeConfigFile = flags.String("kubeconfig", "",
			`Path to a kubeconfig file containing authorization and API server information.`)

		//resyncPeriod = flags.Duration("sync-period", 0,
		//	`Period at which the controller forces the repopulation of its local object stores. Disabled by default.`)
		//
		//watchNamespace = flags.String("watch-namespace", apiv1.NamespaceAll,
		//	`Namespace the controller watches for updates to Kubernetes objects.
		//	This includes Ingresses, Services and all configuration resources. All
		//	namespaces are watched if this parameter is left empty.`)

		//profiling = flags.Bool("profiling", true,
		//	`Enable profiling via web interface host:port/debug/pprof/`)

		//defSSLCertificate = flags.String("default-ssl-certificate", "",
		//	`Secret containing a SSL certificate to be used by the default HTTPS server (catch-all).
		//	Takes the form "namespace/name".`)

		defHealthzURL = flags.String("health-check-path", "/healthz",
			`URL path of the health check endpoint.
Configured inside the NGINX status server. All requests received on the port
defined by the healthz-port parameter are forwarded internally to this path.`)

		showVersion = flags.Bool("version", false,
			`Show release information about the NGINX Ingress controller and exit.`)

		httpPort= flags.Int("http-port", 80, `Port to use for servicing HTTP traffic.`)
		httpsPort= flags.Int("https-port", 443, `Port to use for servicing HTTPS traffic.`)
		statusPort= flags.Int("status-port", 18080, `Port to use for exposing NGINX status pages.`)
		//healthzPort= flags.Int("healthz-port", 10254, "Port to use for the healthz endpoint.")
	)
	flag.Set("logtostderr", "true")

	flags.AddGoFlagSet(flag.CommandLine)
	flags.Parse(os.Args)
	flag.CommandLine.Parse([]string{})

	pflag.VisitAll(func(flag *pflag.Flag) {
		glog.V(2).Infof("FLAG: --%s=%q", flag.Name, flag.Value)
	})

	// check port collisions
	if !ing_net.IsPortAvailable(*httpPort) {
		return false, nil, fmt.Errorf("Port %v is already in use. Please check the flag --http-port", *httpPort)
	}

	if !ing_net.IsPortAvailable(*httpsPort) {
		return false, nil, fmt.Errorf("Port %v is already in use. Please check the flag --https-port", *httpsPort)
	}

	if !ing_net.IsPortAvailable(*statusPort) {
		return false, nil, fmt.Errorf("Port %v is already in use. Please check the flag --status-port", *statusPort)
	}
	config := &Configuration{
		ApiserverHost: 		apiserverHost,
		CommonConf:			commonConf,
		EventConf:			eventConf,
		HttpConf:			httpConf,
		KubeConfigFile: 	kubeConfigFile,
		DefHealthzURL:		defHealthzURL,
		Version:			showVersion,
		ListenPort:		ListenPorts{
			HTTP:			*httpPort,
			HTTPS: 			*httpsPort,
			Status: 		*statusPort,
		},
	}
	return true, config, nil
}
