package main

import (
	"github.com/spf13/pflag"
	"flag"
	"os"
	"github.com/golang/glog"
	"fmt"
	ing_net "k8s.io/extendingress-controller/net"
	"k8s.io/extendingress-controller/cmd"
	"time"
)

const (
	LEASE_DURATION = 13 * time.Second
	RENEW_DURATION = 8 * time.Second
	RETRY_PERIOD_DURATION = 2 * time.Second
)
func parseFlags() (bool, *cmd.Configuration, error) {
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

		lockObjectNamespace = flags.String("lock-object-namespace", "default" ,"Define the namespace of the lock object.")

		lockObjectName = flags.String("lock-object-name", "ingress" ,"Define the name of the lock object.")

		leaderElect = flags.Bool("leader-elect", false, "Start a leader election client and gain leadership before "+
			"executing the main loop. Enable this when running replicated "+
			"components for high availability.")

		leaseDuration = flags.Duration("leader-elect-lease-duration", LEASE_DURATION , "The duration that non-leader candidates will wait after observing a leadership "+
			"renewal until attempting to acquire leadership of a led but unrenewed leader "+
			"slot. This is effectively the maximum duration that a leader can be stopped "+
			"before it is replaced by another candidate. This is only applicable if leader "+
			"election is enabled.")

		renewDuration = flags.Duration("leader-elect-renew-deadline", RENEW_DURATION, "The interval between attempts by the acting master to renew a leadership slot "+
			"before it stops leading. This must be less than or equal to the lease duration. "+
			"This is only applicable if leader election is enabled.")

		retryPeriodDuration = flags.Duration("leader-elect-retry-period", RETRY_PERIOD_DURATION, "The duration the clients should wait between attempting acquisition and renewal "+
			"of a leadership. This is only applicable if leader election is enabled.")

		resourceLock = flags.String("leader-elect-resource-lock", "endpoints","The type of resource object that is used for locking during "+
			"leader election. Supported options are `endpoints` (default) and `configmaps`.")

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
	config := &cmd.Configuration{
		ApiserverHost: 		apiserverHost,
		CommonConf:			commonConf,
		EventConf:			eventConf,
		HttpConf:			httpConf,
		KubeConfigFile: 	kubeConfigFile,
		DefHealthzURL:		defHealthzURL,
		Version:			showVersion,
		ListenPort:		cmd.ListenPorts{
			HTTP:			*httpPort,
			HTTPS: 			*httpsPort,
			Status: 		*statusPort,
		},
		LockObjectNamespace: *lockObjectNamespace,
		LockObjectName: *lockObjectName,
		LeaderElect: *leaderElect,
		LeaseDuration: *leaseDuration,
		RenewDuration: *renewDuration,
		RetryPeriodDuration: *retryPeriodDuration,
		ResourceLock: *resourceLock,
	}
	return true, config, nil
}
