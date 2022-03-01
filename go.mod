module github.com/openshift/oc-mirror

go 1.16

require (
	github.com/blang/semver/v4 v4.0.0
	github.com/bshuster-repo/logrus-logstash-hook v1.0.2 // indirect
	github.com/containerd/containerd v1.5.8
	github.com/containers/image/v5 v5.16.0
	github.com/docker/cli v20.10.12+incompatible
	github.com/docker/distribution v2.7.1+incompatible
	github.com/go-git/go-git/v5 v5.4.2 // indirect
	github.com/google/go-containerregistry v0.8.0
	github.com/google/uuid v1.3.0
	github.com/imdario/mergo v0.3.12
	github.com/joelanford/ignore v0.0.0-20210610194209-63d4919d8fb2
	github.com/mholt/archiver/v3 v3.5.0
	github.com/opencontainers/go-digest v1.0.0
	github.com/opencontainers/image-spec v1.0.2-0.20211117181255-693428a734f5
	github.com/openshift/api v0.0.0-20210831091943-07e756545ac1
	github.com/openshift/library-go v0.0.0-20210831102543-1a08f0c3bd9a
	github.com/openshift/oc v0.0.0-alpha.0.0.20210721184532-4df50be4d929
	github.com/operator-framework/operator-registry v1.19.6-0.20220120140729-354cd3851678
	github.com/sirupsen/logrus v1.8.1
	github.com/spf13/afero v1.7.1
	github.com/spf13/cobra v1.3.0
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.7.0
	golang.org/x/crypto v0.0.0-20211108221036-ceb1ce70b4fa
	gopkg.in/yaml.v2 v2.4.0
	helm.sh/helm/v3 v3.7.2
	k8s.io/apimachinery v0.22.4
	k8s.io/cli-runtime v0.22.4
	k8s.io/client-go v0.22.4
	k8s.io/component-base v0.22.4
	k8s.io/klog v1.0.0
	k8s.io/klog/v2 v2.9.0
	k8s.io/kubectl v0.22.4
	sigs.k8s.io/kustomize/kyaml v0.11.0
	sigs.k8s.io/yaml v1.3.0
)

replace (
	//github.com/Microsoft/hcsshim => github.com/Microsoft/hcsshim v0.8.7
	github.com/apcera/gssapi => github.com/openshift/gssapi v0.0.0-20161010215902-5fb4217df13b
	k8s.io/apimachinery => github.com/openshift/kubernetes-apimachinery v0.0.0-20210730111815-c26349f8e2c9
	k8s.io/cli-runtime => github.com/openshift/kubernetes-cli-runtime v0.0.0-20210730111823-1570202448c3
	k8s.io/client-go => github.com/openshift/kubernetes-client-go v0.0.0-20210730111819-978c4383ac68
	k8s.io/kubectl => github.com/openshift/kubernetes-kubectl v0.0.0-20210730111826-9c6734b9d97d
)
