
BUILD_SHA ?= $(shell git rev-parse --short HEAD)
BUILD_VERSION ?= $(shell git describe --tags $$(git rev-list --tags --max-count=1))

# source controller version
SOURCE_VER ?= v0.31.0

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
GOBIN=$(shell pwd)/bin

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

PHONY: gen-mocks
gen-mocks: mockgen
	${MOCKGEN} -package=mocks -destination=./internal/clients/cloudformation/mocks/mock_sdk.go -source=./internal/clients/cloudformation/sdk_interfaces.go
	${MOCKGEN} -package=mocks -destination=./internal/clients/s3/mocks/mock_sdk.go -source=./internal/clients/s3/sdk_interfaces.go
	${MOCKGEN} -package=mocks -destination=./internal/clients/mocks/mock_clients.go -source=./internal/clients/clients.go
	${MOCKGEN} -package=mocks -destination=./internal/mocks/mock_event_recorder.go k8s.io/client-go/tools/record EventRecorder
	${MOCKGEN} -package=mocks -destination=./internal/mocks/mock_kubernetes_client.go sigs.k8s.io/controller-runtime/pkg/client Client,StatusWriter

test: tidy generate gen-mocks fmt vet manifests
	go test ./... -coverprofile cover.out
	cd api; go test ./... -coverprofile cover.out

view-test-coverage:
	go tool cover -html=cover.out

build: generate gen-mocks fmt vet manifests
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
	SOURCE_CONTROLLER_LOCALHOST=localhost:30000 AWS_REGION=$(AWS_REGION) TEMPLATE_BUCKET=flux-cfn-templates-$(AWS_ACCOUNT_ID)-$(AWS_REGION) go run ./main.go

# Install CRDs into a cluster
install: manifests
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

# Uninstall CRDs from a cluster
uninstall: manifests
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

# Deploy into a local kind cluster - the cluster must already be bootstrapped
deploy-local: install build-docker-image
	docker tag aws-cloudformation-controller-for-flux:latest aws-cloudformation-controller-for-flux:local
	kind load docker-image aws-cloudformation-controller-for-flux:local
	mkdir -p config/dev && cp -r config/default config/crd config/manager config/rbac config/dev/
	cp config/manager/overlays/dev/kustomization.yaml config/dev/manager
ifdef AWS_ACCESS_KEY_ID
	cat config/manager/overlays/aws-creds-from-env-vars/env-patch.yaml | AWS_REGION=$(AWS_REGION) TEMPLATE_BUCKET=flux-cfn-templates-$(AWS_ACCOUNT_ID)-$(AWS_REGION) envsubst > config/dev/manager/env-patch.yaml
else
	cat config/manager/overlays/aws-creds-from-mounted-file/env-patch.yaml | AWS_REGION=$(AWS_REGION) TEMPLATE_BUCKET=flux-cfn-templates-$(AWS_ACCOUNT_ID)-$(AWS_REGION) envsubst > config/dev/manager/env-patch.yaml
endif
	cd config/dev/default && $(KUSTOMIZE) edit set image public.ecr.aws/aws-cloudformation/aws-cloudformation-controller-for-flux=aws-cloudformation-controller-for-flux:local
	$(KUSTOMIZE) build config/dev/default | kubectl apply -f -
	kubectl rollout restart deployment cfn-controller --namespace=flux-system
	kubectl rollout status deployment/cfn-controller --namespace=flux-system --timeout=30s
	kubectl logs deployment/cfn-controller --namespace flux-system
	rm -rf config/dev

bootstrap-local-cluster:
	$(shell pwd)/local-dev/bootstrap-local-kind-cluster.sh

integ-test: generate fmt vet manifests
	go test -v -tags=integration ./internal/integtests/

##### Install dev tools #####

.PHONY: install-tools
install-tools: kustomize controller-gen gen-crd-api-reference-docs

KUSTOMIZE = $(shell pwd)/bin/kustomize
.PHONY: kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v4@v4.5.7)

CONTROLLER_GEN = $(GOBIN)/controller-gen
.PHONY: controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.9.2)

MOCKGEN = $(GOBIN)/mockgen
.PHONY: mockgen
mockgen: ## Download mockgen locally if necessary.
	$(call go-install-tool,$(MOCKGEN),github.com/golang/mock/mockgen@v1.6.0)

GEN_CRD_API_REFERENCE_DOCS = $(GOBIN)/gen-crd-api-reference-docs
.PHONY: gen-crd-api-reference-docs
gen-crd-api-reference-docs:
	$(call go-install-tool,$(GEN_CRD_API_REFERENCE_DOCS),github.com/ahmetb/gen-crd-api-reference-docs@v0.3.0)

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
