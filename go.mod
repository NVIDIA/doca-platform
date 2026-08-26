module github.com/nvidia/doca-platform

go 1.25.0

toolchain go1.25.13

require (
	antrea.io/libOpenflow v0.17.0
	antrea.io/ofnet v0.12.0
	dario.cat/mergo v1.0.1
	github.com/BurntSushi/toml v1.5.0
	github.com/Masterminds/semver/v3 v3.4.0
	github.com/Masterminds/sprig/v3 v3.3.0
	github.com/Mellanox/maintenance-operator/api v0.3.0
	github.com/Mellanox/nic-configuration-operator v1.3.2-0.20260713095731-77a61397f2bb
	github.com/containernetworking/cni v1.2.3
	github.com/containernetworking/plugins v1.5.1
	github.com/diskfs/go-diskfs v1.9.3
	github.com/fatih/color v1.18.0
	github.com/fluxcd/pkg/runtime v0.52.0
	github.com/fsnotify/fsnotify v1.9.0
	github.com/gobuffalo/flect v1.0.3
	github.com/godbus/dbus/v5 v5.1.0
	github.com/golang/glog v1.2.5
	github.com/hashicorp/go-hclog v1.6.3
	github.com/hashicorp/vault/api v1.23.0
	github.com/hashicorp/vault/api/auth/approle v0.12.0
	github.com/hashicorp/vault/api/auth/kubernetes v0.12.0
	github.com/hashicorp/vault/api/auth/userpass v0.12.0
	github.com/j-keck/arping v1.0.3
	github.com/k8snetworkplumbingwg/network-attachment-definition-client v1.7.5
	github.com/k8snetworkplumbingwg/sriovnet v1.3.0
	github.com/kubernetes-csi/csi-lib-utils v0.22.0
	github.com/mcuadros/go-version v0.0.0-20190830083331-035f6764e8d2
	github.com/olekukonko/tablewriter v0.0.5
	github.com/onsi/ginkgo/v2 v2.32.0
	github.com/onsi/gomega v1.42.1
	github.com/openconfig/gnmi v0.12.0
	github.com/openconfig/gnoi v0.6.0
	github.com/opencontainers/go-digest v1.0.0
	github.com/ovn-org/libovsdb v0.7.0
	github.com/spf13/cobra v1.10.0
	github.com/spf13/pflag v1.0.9
	github.com/spiffe/spire-plugin-sdk v1.15.1
	github.com/stretchr/testify v1.11.1
	github.com/vishvananda/netlink v1.3.2-0.20251101063711-6e61cd407d1d
	go.uber.org/mock v0.5.0
	gonum.org/v1/gonum v0.16.0
	google.golang.org/grpc v1.79.3
	gopkg.in/k8snetworkplumbingwg/multus-cni.v4 v4.0.2
	k8s.io/api v0.35.4
	k8s.io/apimachinery v0.35.4
	k8s.io/client-go v0.35.4
	k8s.io/component-base v0.35.4
	k8s.io/component-helpers v0.35.4
	k8s.io/klog/v2 v2.130.1
	k8s.io/kms v0.35.4
	k8s.io/mount-utils v0.35.4
	k8s.io/utils v0.0.0-20251002143259-bc988d571ff4
	oras.land/oras-go/v2 v2.5.0
	sigs.k8s.io/controller-runtime v0.22.3
	sigs.k8s.io/gateway-api v1.4.1
	sigs.k8s.io/yaml v1.6.0
)

require (
	github.com/Mellanox/rdmamap v1.2.0 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/anchore/go-lzo v0.1.0 // indirect
	github.com/cenkalti/hub v1.0.2 // indirect
	github.com/cenkalti/rpc2 v1.0.4 // indirect
	github.com/contiv/libovsdb v0.0.0-20170227191248-d0061a53e358 // indirect
	github.com/coreos/go-iptables v0.7.0 // indirect
	github.com/djherbis/times v1.6.0 // indirect
	github.com/elliotwutingfeng/asciiset v0.0.0-20260129054604-cfde2086bc57 // indirect
	github.com/evanphx/json-patch v5.9.0+incompatible // indirect
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/go-plugin v1.4.0 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.8 // indirect
	github.com/hashicorp/go-rootcerts v1.0.2 // indirect
	github.com/hashicorp/go-secure-stdlib/parseutil v0.2.0 // indirect
	github.com/hashicorp/go-secure-stdlib/strutil v0.1.2 // indirect
	github.com/hashicorp/go-sockaddr v1.0.7 // indirect
	github.com/hashicorp/hcl v1.0.1-vault-7 // indirect
	github.com/hashicorp/yamux v0.0.0-20180604194846-3520598351bb // indirect
	github.com/jaypipes/ghw v0.24.0 // indirect
	github.com/jaypipes/pcidb v1.1.1 // indirect
	github.com/klauspost/compress v1.18.5 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/mitchellh/go-testing-interface v0.0.0-20171004221916-a61a99592b77 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/oklog/run v1.0.0 // indirect
	github.com/orcaman/concurrent-map/v2 v2.0.1 // indirect
	github.com/pierrec/lz4/v4 v4.1.26 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pkg/xattr v0.4.12 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/ryanuber/go-glob v1.0.0 // indirect
	github.com/safchain/ethtool v0.4.0 // indirect
	github.com/sirupsen/logrus v1.9.4 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/ulikunitz/xz v0.5.15 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	go.yaml.in/yaml/v2 v2.4.3 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/mod v0.36.0 // indirect
	howett.net/plist v1.0.2-0.20250314012144-ee69052608d9 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v6 v6.3.0 // indirect
)

require (
	cel.dev/expr v0.25.1 // indirect
	github.com/Masterminds/goutils v1.1.1 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/fxamacker/cbor/v2 v2.9.0 // indirect
	github.com/go-resty/resty/v2 v2.16.3
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/gorilla/websocket v1.5.4-0.20250319132907-e064f32e3674 // indirect
	github.com/huandu/xstrings v1.5.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-runewidth v0.0.14 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/moby/spdystream v0.5.1 // indirect
	github.com/moby/sys/mountinfo v0.7.2 // indirect
	github.com/mxk/go-flowrate v0.0.0-20140419014527-cca7078d478f // indirect
	github.com/openconfig/bootz v0.3.1 // indirect
	github.com/openconfig/gnsi v1.7.0 // indirect
	github.com/opencontainers/image-spec v1.1.0
	github.com/rivo/uniseg v0.2.0 // indirect
	github.com/shopspring/decimal v1.4.0 // indirect
	github.com/spf13/cast v1.7.1 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	golang.org/x/crypto v0.53.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	gopkg.in/evanphx/json-patch.v4 v4.13.0 // indirect
)

require (
	antrea.io/antrea v1.15.2
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/container-storage-interface/spec v1.11.0
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/emicklei/go-restful/v3 v3.13.0
	github.com/evanphx/json-patch/v5 v5.9.11 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fluxcd/pkg/apis/meta v1.9.0 // indirect
	github.com/go-logr/logr v1.4.3
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-logr/zapr v1.3.0 // indirect
	github.com/go-openapi/jsonpointer v0.21.2 // indirect
	github.com/go-openapi/jsonreference v0.21.0 // indirect
	github.com/go-openapi/swag v0.23.1 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/google/cel-go v0.26.0 // indirect
	github.com/google/gnostic-models v0.7.0 // indirect
	github.com/google/go-cmp v0.7.0
	github.com/google/pprof v0.0.0-20260402051712-545e8a4df936 // indirect
	github.com/google/uuid v1.6.0
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.26.3 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/mailru/easyjson v0.9.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_golang v1.23.2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.66.1
	github.com/prometheus/procfs v0.17.0 // indirect
	github.com/stoewer/go-strcase v1.3.0 // indirect
	github.com/vishvananda/netns v0.0.5 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.61.0 // indirect
	go.opentelemetry.io/otel v1.43.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.34.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.34.0 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	go.opentelemetry.io/otel/sdk v1.43.0 // indirect
	go.opentelemetry.io/otel/trace v1.43.0 // indirect
	go.opentelemetry.io/proto/otlp v1.5.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.28.0 // indirect
	golang.org/x/exp v0.0.0-20241217172543-b2144cdd0a67 // indirect
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/oauth2 v0.34.0 // indirect
	golang.org/x/sys v0.46.0
	golang.org/x/term v0.44.0
	golang.org/x/text v0.38.0 // indirect
	golang.org/x/time v0.12.0 // indirect
	golang.org/x/tools v0.45.0 // indirect
	gomodules.xyz/jsonpatch/v2 v2.4.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20251202230838-ff82c1b0f217 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260401024825-9d38bb4040a9 // indirect
	google.golang.org/protobuf v1.36.11
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v3 v3.0.1
	k8s.io/apiextensions-apiserver v0.35.4
	k8s.io/apiserver v0.35.4
	k8s.io/klog v1.0.0 // indirect
	k8s.io/kube-openapi v0.0.0-20250910181357-589584f1c912 // indirect
	k8s.io/kubelet v0.31.4
	sigs.k8s.io/apiserver-network-proxy/konnectivity-client v0.31.2 // indirect
	sigs.k8s.io/json v0.0.0-20250730193827-2d320260d730 // indirect
)
