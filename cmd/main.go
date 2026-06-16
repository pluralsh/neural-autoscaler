/*
Copyright 2026.

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
	"context"
	"net/http"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	autoscalingv1alpha1 "github.com/pluralsh/neural-autoscaler/api/v1alpha1"
	"github.com/pluralsh/neural-autoscaler/cmd/args"
	"github.com/pluralsh/neural-autoscaler/internal/controller"
	"github.com/pluralsh/neural-autoscaler/internal/forecast"
	"github.com/pluralsh/neural-autoscaler/internal/forecast/onnx"
	"github.com/pluralsh/neural-autoscaler/internal/metrics"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(autoscalingv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	args.Init()

	var forecaster forecast.Forecaster
	if args.ForecastEnabled() {
		var err error
		forecaster, err = onnx.New(forecast.Config{Options: args.ForecastOptions()})
		if err != nil {
			setupLog.Error(err, "unable to load ONNX forecast model")
			os.Exit(1)
		}
		defer func() {
			if err := forecaster.Close(); err != nil {
				setupLog.Error(err, "unable to close ONNX forecast model")
			}
		}()
		setupLog.Info("loaded ONNX forecast model", "family", args.ModelFamily(), "path", args.ModelPath())
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: args.MetricsAddr()},
		HealthProbeBindAddress: args.ProbeAddr(),
		LeaderElection:         args.EnableLeaderElection(),
		LeaderElectionID:       "d90e9ec2.plural.sh",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	metricsClient, err := metricsclientset.NewForConfig(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "unable to create metrics-server client")
		os.Exit(1)
	}

	metricsFactory := &metrics.Factory{
		K8sClient:     mgr.GetClient(),
		MetricsClient: metrics.NewMetricsClientAdapter(metricsClient),
		History:       metrics.NewHistoryStore(metrics.DefaultHistoryCapacity),
	}

	if err = (&controller.NeuralAutoscalerReconciler{
		Client:         mgr.GetClient(),
		Scheme:         mgr.GetScheme(),
		Forecaster:     forecaster,
		MetricsFactory: metricsFactory,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "NeuralAutoscaler")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}
	if forecaster != nil {
		if checker, ok := forecaster.(forecast.HealthChecker); ok {
			if err := mgr.AddReadyzCheck("forecaster", func(_ *http.Request) error {
				return checker.Ready(context.Background())
			}); err != nil {
				setupLog.Error(err, "unable to set up forecaster ready check")
				os.Exit(1)
			}
		}
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
