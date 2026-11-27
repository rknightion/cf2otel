set shell := ["bash", "-euo", "pipefail", "-c"]

version := `git describe --tags --always --dirty 2>/dev/null || echo dev`
commit := `git rev-parse HEAD 2>/dev/null || echo unknown`
build_date := `date -u +%Y-%m-%dT%H:%M:%SZ`
ldflags := "-s -w -X main.version=" + version + " -X main.commit=" + commit + " -X main.buildDate=" + build_date
tools_dir := justfile_directory() + "/.tools"

# renovate: datasource=go depName=github.com/golangci/golangci-lint/v2
golangci_lint_version := "v2.15.0"
# renovate: datasource=go depName=golang.org/x/vuln
govulncheck_version := "v1.10.0"
# renovate: datasource=go depName=github.com/goreleaser/goreleaser/v2
goreleaser_version := "v2.20.3"
# renovate: datasource=npm depName=groma.md
groma_version := "0.7.8"
cloud_helm_version := "v4.2.2"
cloud_kubeconform_version := "v0.8.0"
cloud_helm := tools_dir + "/helm-" + cloud_helm_version + "/helm"
cloud_kubeconform_dir := tools_dir + "/kubeconform-" + cloud_kubeconform_version
cloud_crd_schema := "https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json"
# renovate: datasource=git-refs depName=https://github.com/rknightion/fleet-review currentValue=main
fleet_review_rev := "4fd98c6d90a78836eedc28520ba7f9c8966ff70b"
golangci_lint_dir := tools_dir + "/golangci-lint-" + golangci_lint_version

# List available recipes
default:
    @just --list </dev/null

# Install pinned tools and download modules
setup: _install-golangci-lint
    go mod download

# Warm the source checks in Cursor Cloud without running tests or starting cf2otel.
cloud-bootstrap: setup _cloud-install-helm
    #!/usr/bin/env bash
    set -euo pipefail
    scratch="$(mktemp -d)"
    trap 'rm -rf "$scratch"' EXIT
    mkdir -p {{ quote(cloud_kubeconform_dir) }}
    GOBIN={{ quote(cloud_kubeconform_dir) }} go install github.com/yannh/kubeconform/cmd/kubeconform@{{ cloud_kubeconform_version }}
    GOBIN={{ quote(tools_dir) }} go install golang.org/x/vuln/cmd/govulncheck@{{ govulncheck_version }}
    npx -y groma.md@{{ groma_version }} plugin install
    go test -race -c -o "$scratch/" ./...

[private]
_cloud-install-helm:
    #!/usr/bin/env bash
    set -euo pipefail
    helm={{ quote(cloud_helm) }}
    if [[ -x "$helm" ]] && [[ "$("$helm" version --short)" == "{{ cloud_helm_version }}"+* ]]; then
      exit 0
    fi
    arch="$(go env GOARCH)"
    case "$arch" in
      amd64) checksum=9adafecab4d406853bba163a70e9f104f47dbbf65ce24b7653bae7e36150bcb6 ;;
      arm64) checksum=78803142087a0069fa4b50d3f32a84d3ef25c14d1ee8a40fbccf86a6216d2f36 ;;
      *) printf 'Unsupported Helm architecture: %s\n' "$arch" >&2; exit 1 ;;
    esac
    scratch="$(mktemp -d)"
    trap 'rm -rf "$scratch"' EXIT
    archive="helm-{{ cloud_helm_version }}-linux-$arch.tar.gz"
    curl -qfsSL --retry 2 --max-time 120 -o "$scratch/$archive" "https://get.helm.sh/$archive"
    (cd "$scratch" && printf '%s  %s\n' "$checksum" "$archive" | sha256sum --check)
    tar -xzf "$scratch/$archive" -C "$scratch" "linux-$arch/helm"
    install -D -m 0755 "$scratch/linux-$arch/helm" "$helm"

# Validate the default chart against the same Kubernetes floor and schemas as CI.
[group('check')]
cloud-helm-check:
    {{ quote(cloud_helm) }} lint charts/cf2otel
    {{ quote(cloud_helm) }} template test charts/cf2otel | {{ quote(cloud_kubeconform_dir + "/kubeconform") }} -strict -summary -kubernetes-version 1.29.0 -schema-location default -schema-location {{ quote(cloud_crd_schema) }} -

[private]
_install-golangci-lint:
    mkdir -p {{ golangci_lint_dir }}
    test -x {{ golangci_lint_dir }}/golangci-lint || GOBIN={{ golangci_lint_dir }} go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@{{ golangci_lint_version }}

# Format Go sources in place
[group('dev')]
fmt:
    gofmt -w $(find . -name '*.go' -not -path './vendor/*' -not -path './.tools/*')

# Fail if Go sources or the justfile need formatting
[group('check')]
[no-exit-message]
fmt-check:
    files="$(gofmt -l $(find . -name '*.go' -not -path './vendor/*' -not -path './.tools/*'))"; test -z "$files" || { echo "Go files require formatting:"; echo "$files"; exit 1; }
    just --fmt --check </dev/null

# Run golangci-lint
[group('check')]
[no-exit-message]
lint: _install-golangci-lint
    {{ golangci_lint_dir }}/golangci-lint run --allow-parallel-runners ./...

# Run go vet
[group('check')]
[no-exit-message]
vet:
    go vet ./...

# Run the race-enabled test suite, optionally filtered by test name
[group('check')]
[no-exit-message]
test filter="":
    if [ -n "{{ filter }}" ]; then go test -race -run '{{ filter }}' ./...; else go test -race ./...; fi

# Fail if go.mod or go.sum are untidy
[group('check')]
[no-exit-message]
tidy-check:
    go mod tidy -diff

# Tidy go.mod and go.sum
[group('dev')]
tidy:
    go mod tidy

# Build the binary into bin/
[group('build')]
build:
    go build -trimpath -ldflags "{{ ldflags }}" -o bin/cf2otel ./cmd/cf2otel

# Regenerate Grafana dashboard and alert manifests
[group('gen')]
gen:
    python3 grafana/build_dashboard.py
    python3 grafana/build_rules.py

# Verify generated Grafana manifests match their source
[group('check')]
[no-exit-message]
gen-check:
    python3 grafana/build_dashboard.py --check
    python3 grafana/build_rules.py --check

# Run govulncheck
[group('check')]
[no-exit-message]
vuln:
    go run golang.org/x/vuln/cmd/govulncheck@{{ govulncheck_version }} ./...

# The pre-commit gate: everything that runs with only the Go toolchain
[group('check')]
check: fmt-check lint vet test tidy-check build vuln gen-check verify-deployment-test

# Confirm the requested revision through the provisioned Grafana read path.
# Set CF2OTEL_DEPLOY_QUERY_URL and CF2OTEL_DEPLOY_TOKEN_FILE on the authorized
# landing host. Run loop-land verify after edge publication and the runtime update.
verify-deployment sha:
    @python3 scripts/verify_deployment.py {{ quote(sha) }}

# Exercise the deployment verifier without a live provider or real credentials.
verify-deployment-test:
    python3 -m unittest discover -s scripts -p test_verify_deployment.py

# Build release archives locally without publishing
[group('build')]
[no-exit-message]
snapshot:
    go run github.com/goreleaser/goreleaser/v2@{{ goreleaser_version }} release --snapshot --clean --skip=publish,sign,sbom,docker

# Build the container image
[group('build')]
image tag="cf2otel:dev":
    docker build --build-arg VERSION={{ version }} --build-arg COMMIT={{ commit }} --build-arg BUILD_DATE={{ build_date }} -t {{ tag }} .

# CI superset: check plus goreleaser (cross-compilation) and the image (Docker daemon)
[group('check')]
ci: check snapshot image

# GitHub issues and the rknightion Project; maintainers only (needs the tracker tools and credentials)
[group('tracker')]
[positional-arguments]
tracker *args:
    #!/usr/bin/env bash
    set -euo pipefail
    export TRACKER_REPO=rknightion/cf2otel TRACKER_PROJECT_OWNER=rknightion TRACKER_PROJECT=7
    export TRACKER_ISSUE_IDENTITY=app TRACKER_PROJECT_IDENTITY=user
    exec tracker-with-token "$@"

# print the issue a legacy board id (CFO-0001) became
[group('tracker')]
issue id:
    TRACKER_REPO=rknightion/cf2otel tracker legacy {{ quote(id) }}

# Remove build output and downloaded tools
[group('build')]
clean:
    rm -rf bin dist {{ tools_dir }}

# Scan newly reachable history and the net diff before a public push.
[group('check')]
[positional-arguments]
push-scan base head:
    go run ./tools/pushscan "$1" "$2"

# Check a release notes file against conventional commits since a release tag.
[group('release')]
relnotes-check notes_file commit_range="":
    go run ./tools/relnotes --range "{{ commit_range }}" --notes "{{ notes_file }}"

# Advisory report of duplication and complexity a change introduced against base (merge-base)
[group('check')]
review base="origin/main":
    uvx --from "git+https://github.com/rknightion/fleet-review@{{ fleet_review_rev }}" fleet-review --base {{ base }}

# Rescan source into the Groma architecture map in .groma/
[group('dev')]
groma-scan:
    npx -y groma.md@{{ groma_version }} plugin install
    npx -y groma.md@{{ groma_version }} scan

# Fail if the Groma architecture map has drifted from the source tree
[group('check')]
[no-exit-message]
groma-check:
    #!/usr/bin/env bash
    set -euo pipefail
    groma() { npx -y "groma.md@{{ groma_version }}" "$@"; }
    groma plugin install
    plugins="$(groma plugin list)"
    printf '%s\n' "$plugins"
    if grep -q missing <<<"$plugins" || ! grep -q found <<<"$plugins"; then
        echo "groma-check: a scanner is missing, so a scan would assert nothing" >&2
        exit 1
    fi
    groma scanner check
    report="$(groma scan)"
    printf '%s\n' "$report"
    seen="$(sed -nE 's/^created ([0-9]+), refreshed ([0-9]+), matched ([0-9]+).*/\1 \2 \3/p' <<<"$report" | awk '{print $1 + $2 + $3}')"
    if [ -z "$seen" ] || [ "$seen" -eq 0 ]; then
        echo "groma-check: the scan reported no components" >&2
        exit 1
    fi
    # groma scan returns while a detached "scanner refresh" child still holds .groma/.groma.lock.
    for _ in $(seq 1 120); do
        [ -e .groma/.groma.lock ] || break
        sleep 0.5
    done
    if [ -e .groma/.groma.lock ]; then
        echo "groma-check: .groma/.groma.lock is still held after 60 s" >&2
        exit 1
    fi
    fail=0
    drift="$(git status --porcelain --untracked-files=all -- .groma ':(exclude).groma/.groma.lock')"
    if [ -n "$drift" ]; then
        echo "groma-check: .groma/ differs from HEAD; run 'just groma-scan', curate, and commit:" >&2
        printf '%s\n' "$drift" >&2
        fail=1
    fi
    empty="$(find .groma -path '*/components/*.md' -exec grep -L '^      file:' {} + || true)"
    if [ -n "$empty" ]; then
        echo "groma-check: components with an empty code list (groma remove <id> or combine):" >&2
        printf '%s\n' "$empty" >&2
        fail=1
    fi
    stale="$(find .groma -path '*/components/*.md' -exec sed -nE 's/^      file: (.*)$/\1/p' {} + | sort -u | while read -r f; do [ -e "$f" ] || echo "$f"; done)"
    if [ -n "$stale" ]; then
        echo "groma-check: code references to files that no longer exist:" >&2
        printf '%s\n' "$stale" >&2
        fail=1
    fi
    exit "$fail"
