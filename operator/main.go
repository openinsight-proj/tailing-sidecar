/*
Copyright 2021.

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
	"flag"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	tailingsidecarv1 "github.com/SumoLogic/tailing-sidecar/operator/api/v1"
	"github.com/SumoLogic/tailing-sidecar/operator/controllers"
	"github.com/SumoLogic/tailing-sidecar/operator/handler"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(tailingsidecarv1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var tailingSidecarImage string
	var configPath string
	var config Config
	var err error

	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&tailingSidecarImage, "tailing-sidecar-image", "", "tailing sidecar image")
	flag.StringVar(&configPath, "config", "", "Path to the configuration file")
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	config = GetDefaultConfig()
	if configPath != "" {
		err = ReadConfig(configPath, &config)
		if err != nil {
			setupLog.Error(err, "unable to read configuration", "configPath", configPath)
			os.Exit(1)
		}
	}

	if err := config.Validate(); err != nil {
		setupLog.Error(err, "configuration error", "configPath", configPath)
		os.Exit(1)
	}

	if tailingSidecarImage != "" {
		config.Sidecar.Image = tailingSidecarImage
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		Port:               9443,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   "7b555970.sumologic.com",
		LeaseDuration:      (*time.Duration)(&config.LeaderElection.LeaseDuration),
		RenewDeadline:      (*time.Duration)(&config.LeaderElection.RenewDeadline),
		RetryPeriod:        (*time.Duration)(&config.LeaderElection.RetryPeriod),
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controllers.TailingSidecarConfigReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("TailingSidecarConfig"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "TailingSidecarConfig")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	mgr.GetWebhookServer().Register("/add-tailing-sidecars-v1-pod", &webhook.Admission{
		Handler: &handler.PodExtender{
			Client:                  mgr.GetClient(),
			TailingSidecarImage:     config.Sidecar.Image,
			TailingSidecarResources: config.Sidecar.Resources,
			ConfigMapName:           config.Sidecar.Config.Name,
			ConfigMountPath:         config.Sidecar.Config.MountPath,
			ConfigMapNamespace:      config.Sidecar.Config.Namespace,
		},
	})

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
