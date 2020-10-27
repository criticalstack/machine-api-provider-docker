module github.com/criticalstack/machine-api-provider-docker

go 1.14

require (
	github.com/criticalstack/crit v1.0.3
	github.com/criticalstack/machine-api v1.0.3
	github.com/go-logr/logr v0.1.0
	github.com/go-openapi/spec v0.19.3
	github.com/onsi/ginkgo v1.12.1
	github.com/onsi/gomega v1.10.1
	github.com/pkg/errors v0.9.1
	go.uber.org/zap v1.15.0
	k8s.io/api v0.18.5
	k8s.io/apimachinery v0.18.5
	k8s.io/client-go v0.18.5
	sigs.k8s.io/cluster-api v0.3.6
	sigs.k8s.io/controller-runtime v0.6.0
	sigs.k8s.io/kind v0.8.1
)
