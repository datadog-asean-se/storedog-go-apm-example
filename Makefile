NAMESPACE ?= storedog
IMAGE     ?= ghcr.io/datadog-asean-se/storedog-go-apm-example:latest
OVERLAY   ?= base

.PHONY: deploy deploy-local build status logs destroy
deploy:        ## Pull the prebuilt image from GHCR and deploy to $(NAMESPACE) (no Docker needed)
	IMAGE=$(IMAGE) NAMESPACE=$(NAMESPACE) OVERLAY=$(OVERLAY) ./scripts/deploy.sh
deploy-local:  ## Build the image locally with Docker, load it into the cluster, deploy
	BUILD=1 NAMESPACE=$(NAMESPACE) OVERLAY=$(OVERLAY) ./scripts/deploy.sh
build:         ## Build the image only
	DOCKER_BUILDKIT=1 docker build -t storedog-go-shop:local .
status:        ## Show pods and recent gateway logs
	kubectl -n $(NAMESPACE) get pods -l 'app in (shop-gateway,shop-inventory,shop-payment,shop-loadgen)'
logs:          ## Tail gateway logs (JSON, with dd.trace_id)
	kubectl -n $(NAMESPACE) logs deploy/shop-gateway --tail=20 -f
destroy:       ## Remove everything this repo deployed
	kubectl -n $(NAMESPACE) delete -k deploy/base --ignore-not-found
