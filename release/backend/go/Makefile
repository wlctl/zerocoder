# dev-agent — Go (первичный таргет)
GO ?= /usr/local/go/bin/go
BUILDDIR := build

# Форматные парсеры/постобработчики, собираемые как Go-плагины (.so).
# text (парсер) и base (постобработчик) остаются built-in в хосте (fallback).
PARSER_FORMATS    := oracle weblogic wls_stdout java access odl
POSTPROC_FORMATS  := oracle weblogic wls_stdout odl

.PHONY: all build vet fmt test test-race cover tidy clean plugins parsers postprocessors dist

all: build

build:
	$(GO) build -o $(BUILDDIR)/ ./cmd/...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w .
	@command -v goimports >/dev/null 2>&1 && goimports -w . || true

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

cover:
	$(GO) test -cover ./...

tidy:
	$(GO) mod tidy

# Сборка всех .so-плагинов в parsers/ и postprocessors/. Go plugins: Linux-only,
# один тулчейн, те же зависимости (парсеры в том же модуле).
plugins: parsers postprocessors

parsers:
	@mkdir -p parsers
	@for f in $(PARSER_FORMATS); do \
	  echo "  plugin  parsers/$$f.so"; \
	  $(GO) build -buildmode=plugin -tags plugin -o parsers/$$f.so ./parsers/$$f || exit 1; \
	done

postprocessors:
	@mkdir -p postprocessors
	@for f in $(POSTPROC_FORMATS); do \
	  echo "  plugin  postprocessors/$$f.so"; \
	  $(GO) build -buildmode=plugin -tags plugin -o postprocessors/$$f.so ./postprocessors/$$f || exit 1; \
	done

# Сборка дистрибутива (бинарник + плагины). Сам ZIP-пакет собирается скриптом
# sh/make-dist.sh в корне проекта.
dist: build plugins

clean:
	rm -rf $(BUILDDIR)/*
	rm -f parsers/*.so postprocessors/*.so