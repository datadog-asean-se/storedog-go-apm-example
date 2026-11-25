NAMESPACE ?= storedog
VERSION   := $(shell cat VERSION)
IMAGE     ?= ghcr.io/datadog-asean-se/storedog-go-apm-example:$(VERSION)
OVERLAY   ?= base

.PHONY: deploy deploy-versions deploy-local build set-version version status logs destroy
deploy:        ## Pull the prebuilt image from GHCR and deploy to $(NAMESPACE) (no Docker needed)
	IMAGE=$(IMAGE) NAMESPACE=$(NAMESPACE) OVERLAY=$(OVERLAY) ./scripts/deploy.sh
deploy-versions: ## Deploy three gateway versions (1.1.0 healthy, 1.2.0 errors, 1.3.0 slow) to compare in APM
	IMAGE=$(IMAGE) NAMESPACE=$(NAMESPACE) OVERLAY=overlays/versions ./scripts/deploy.sh
deploy-local:  ## Build the image locally with Docker, load it into the cluster, deploy
	BUILD=1 NAMESPACE=$(NAMESPACE) OVERLAY=$(OVERLAY) ./scripts/deploy.sh
build:         ## Build the image only
	DOCKER_BUILDKIT=1 docker build --build-arg VERSION=$(VERSION) -t storedog-go-shop:local .
version:       ## Print the app version
	@echo $(VERSION)
set-version:   ## Change the version everywhere: make set-version V=1.1.0
	./scripts/set-version.sh $(V)
status:        ## Show pods and recent gateway logs
	kubectl -n $(NAMESPACE) get pods -l 'app in (shop-gateway,shop-gateway-v2,shop-gateway-v3,shop-inventory,shop-payment,shop-loadgen)' -L tags.datadoghq.com/version
logs:          ## Tail gateway logs (JSON, with dd.trace_id)
	kubectl -n $(NAMESPACE) logs deploy/shop-gateway --tail=20 -f
destroy:       ## Remove everything this repo deployed
	kubectl -n $(NAMESPACE) delete -k deploy/overlays/versions --ignore-not-found
