
BUILD_SHA ?= $(shell git rev-parse --short HEAD)
BUILD_VERSION ?= $(shell git describe --tags $$(git rev-list --tags --max-count=1))

# source controller version
SOURCE_VER ?= v0.31.0

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
GOBIN=$(shell pwd)/bin

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

# Allows for defining additional Docker buildx arguments, e.g. '--push'.
BUILD_ARGS ?=

AWS_ACCOUNT_ID="$(shell aws sts get-caller-identity --query 'Account' --output text)"
AWS_REGION=us-west-2

all: build

##### Generate CRDs #####

# Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
generate: controller-gen
	cd api; $(CONTROLLER_GEN) object:headerFile="../hack/boilerplate.go.txt" paths="./..."

# Generate manifests e.g. CRD, RBAC, etc.
manifests: controller-gen
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config="config/crd/bases"
	cd api; $(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config="../config/crd/bases"

##### Clean up code #####

tidy:
	cd api; rm -f go.sum; go mod tidy -compat=1.19
	rm -f go.sum; go mod tidy -compat=1.19

fmt:
	go fmt ./...

vet:
	go vet ./...

clean:
	go clean ./...
	rm -rf config/crd/bases

##### Build and test #####

KUBEBUILDER_ASSETS?="$(shell $(ENVTEST) --arch=$(ENVTEST_ARCH) use -i $(ENVTEST_KUBERNETES_VERSION) --bin-dir=$(ENVTEST_ASSETS_DIR) -p path)"

test: tidy generate fmt vet manifests envtest
	KUBEBUILDER_ASSETS=$(KUBEBUILDER_ASSETS) go test ./... -coverprofile cover.out
	cd api; go test ./... -coverprofile cover.out

build: generate fmt vet manifests
	go build -o bin/manager \
 		-ldflags "-X main.BuildSHA=$(BUILD_SHA) -X main.BuildVersion=$(BUILD_VERSION)" \
 		main.go

build-docker-image:
	docker build \
		-t "aws-cloudformation-controller-for-flux:latest" \
		-f "./Dockerfile" \
		--build-arg BUILD_SHA="$(BUILD_SHA)" \
		--build-arg BUILD_VERSION="$(BUILD_VERSION)" \
		"."

push-docker-image-to-ecr:
	aws ecr get-login-password --region $(AWS_REGION) | docker login --username AWS --password-stdin $(AWS_ACCOUNT_ID).dkr.ecr.$(AWS_REGION).amazonaws.com
	docker tag aws-cloudformation-controller-for-flux:latest $(AWS_ACCOUNT_ID).dkr.ecr.$(AWS_REGION).amazonaws.com/aws-cloudformation-controller-for-flux:latest
	docker push $(AWS_ACCOUNT_ID).dkr.ecr.$(AWS_REGION).amazonaws.com/aws-cloudformation-controller-for-flux:latest

##### Run locally #####

# Run a controller from your host.
run: generate fmt vet install
	SOURCE_CONTROLLER_LOCALHOST=localhost:30000 AWS_REGION=$(AWS_REGION) TEMPLATE_BUCKET=flux-templates-$(AWS_ACCOUNT_ID) go run ./main.go

# Install CRDs into a cluster
install: manifests
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

# Uninstall CRDs from a cluster
uninstall: manifests
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

# Deploy into cluster - the cluster must already have Flux installed
deploy: manifests build-docker-image push-docker-image-to-ecr
	# Note that this token will expire, so this is only for development use
	kubectl create secret docker-registry ecr-cred --docker-server=$(AWS_ACCOUNT_ID).dkr.ecr.$(AWS_REGION).amazonaws.com --docker-username=AWS --docker-password=$(shell aws ecr get-login-password --region $(AWS_REGION)) -n flux-system
	kubectl patch serviceaccount default -p "{\"imagePullSecrets\": [{\"name\": \"ecr-cred\"}]}" -n flux-system

	mkdir -p config/dev && cp config/default/* config/dev
	cd config/dev && $(KUSTOMIZE) edit set image public.ecr.aws/aws-cloudformation/aws-cloudformation-controller-for-flux=$(AWS_ACCOUNT_ID).dkr.ecr.$(AWS_REGION).amazonaws.com/aws-cloudformation-controller-for-flux:latest
	$(KUSTOMIZE) build config/dev | kubectl apply -f -
	rm -rf config/dev

##### Install dev tools #####

.PHONY: install-tools
install-tools: kustomize controller-gen gen-crd-api-reference-docs install-envtest

KUSTOMIZE = $(shell pwd)/bin/kustomize
.PHONY: kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v4@v4.5.7)

CONTROLLER_GEN = $(GOBIN)/controller-gen
.PHONY: controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.9.2)

GEN_CRD_API_REFERENCE_DOCS = $(GOBIN)/gen-crd-api-reference-docs
.PHONY: gen-crd-api-reference-docs
gen-crd-api-reference-docs:
	$(call go-install-tool,$(GEN_CRD_API_REFERENCE_DOCS),github.com/ahmetb/gen-crd-api-reference-docs@v0.3.0)

ENVTEST_ARCH ?= amd64

ENVTEST_ASSETS_DIR=$(shell pwd)/testbin
ENVTEST_KUBERNETES_VERSION?=latest
install-envtest: setup-envtest
	mkdir -p ${ENVTEST_ASSETS_DIR}
	$(ENVTEST) use $(ENVTEST_KUBERNETES_VERSION) --arch=$(ENVTEST_ARCH) --bin-dir=$(ENVTEST_ASSETS_DIR)

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST = $(shell pwd)/bin/setup-envtest
.PHONY: envtest
setup-envtest: ## Download envtest-setup locally if necessary.
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest@latest)

# go-install-tool will 'go install' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-install-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin go install $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef
