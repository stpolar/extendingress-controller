/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"time"

	"github.com/golang/glog"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	// Uncomment the following line to load the gcp plugin (only required to authenticate against GKE clusters).
	// _ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	clientset "k8s.io/extendingress-controller/pkg/client/clientset/versioned"
	informers "k8s.io/extendingress-controller/pkg/client/informers/externalversions"
	"k8s.io/sample-controller/pkg/signals"
	"runtime"
)



func main() {
	runtime.GOMAXPROCS(3)
	resetNginxComByOS()
	_, conf, err := parseFlags()
	if err != nil {
		glog.Fatal(err)
	}

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	cfg, err := clientcmd.BuildConfigFromFlags(*conf.ApiserverHost, *conf.KubeConfigFile)
	if err != nil {
		glog.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		glog.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}

	extendIngressClient, err := clientset.NewForConfig(cfg)
	if err != nil {
		glog.Fatalf("Error building extendIngress clientset: %s", err.Error())
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Second* 0)
	extendIngressInformerFactory := informers.NewSharedInformerFactory(extendIngressClient, time.Second* 0)

	controller := NewController(kubeClient, extendIngressClient,
		kubeInformerFactory.Core().V1().Endpoints(),
		kubeInformerFactory.Core().V1().ConfigMaps(),
		extendIngressInformerFactory.Extendingresscontroller().V1alpha1().ExtendIngresses(),
		conf)

	go kubeInformerFactory.Start(stopCh)
	go extendIngressInformerFactory.Start(stopCh)

	if err = controller.Run(1, stopCh); err != nil {
		glog.Fatalf("Error running controller: %s", err.Error())
	}
}