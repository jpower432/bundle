declare -a TESTCASES
TESTCASES[1]="full_catalog"
TESTCASES[2]="headsonly_diff"
TESTCASES[3]="registry_backend"
TESTCASES[4]="mirror_to_mirror"
TESTCASES[5]="mirror_to_mirror_nostorage"
TESTCASES[6]="custom_namespace"

# Test full catalog mode.
function full_catalog () {
    run_full imageset-config-full.yaml false
    check_bundles localhost:${REGISTRY_DISCONN_PORT}/${CATALOGNAMESPACE}:test-catalog-latest \
    "bar.v0.1.0 bar.v0.2.0 bar.v1.0.0 baz.v1.0.0 baz.v1.0.1 baz.v1.1.0 foo.v0.1.0 foo.v0.2.0 foo.v0.3.0 foo.v0.3.1" \
    localhost:${REGISTRY_DISCONN_PORT}
}

# Test heads-only mode
function headsonly_diff () {
    run_full imageset-config-headsonly.yaml true
    check_bundles localhost:${REGISTRY_DISCONN_PORT}/${CATALOGNAMESPACE}:test-catalog-latest \
    "bar.v0.1.0 bar.v0.2.0 bar.v1.0.0 baz.v1.1.0 foo.v0.3.1" \
    localhost:${REGISTRY_DISCONN_PORT}

    run_diff imageset-config-headsonly.yaml
    check_bundles localhost:${REGISTRY_DISCONN_PORT}/${CATALOGNAMESPACE}:test-catalog-latest \
    "bar.v0.1.0 bar.v0.2.0 bar.v1.0.0 baz.v1.1.0 foo.v0.3.1 foo.v0.3.2" \
    localhost:${REGISTRY_DISCONN_PORT}
}

# Test registry backend
function registry_backend () {
    run_full imageset-config-headsonly-backend-registry.yaml true
    check_bundles localhost:${REGISTRY_DISCONN_PORT}/${CATALOGNAMESPACE}:test-catalog-latest \
    "bar.v0.1.0 bar.v0.2.0 bar.v1.0.0 baz.v1.1.0 foo.v0.3.1" \
    localhost:${REGISTRY_DISCONN_PORT}

    run_diff imageset-config-headsonly-backend-registry.yaml
    check_bundles localhost:${REGISTRY_DISCONN_PORT}/${CATALOGNAMESPACE}:test-catalog-latest \
    "bar.v0.1.0 bar.v0.2.0 bar.v1.0.0 baz.v1.1.0 foo.v0.3.1 foo.v0.3.2" \
    localhost:${REGISTRY_DISCONN_PORT}
}

# Test mirror to mirror with local backend
function mirror_to_mirror() {
    mirror2mirror imageset-config-headsonly.yaml
    check_bundles localhost:${REGISTRY_DISCONN_PORT}/${CATALOGNAMESPACE}:test-catalog-latest \
    "bar.v0.1.0 bar.v0.2.0 bar.v1.0.0 baz.v1.1.0 foo.v0.3.1" \
    localhost:${REGISTRY_DISCONN_PORT}
}

# Test mirror to mirror no backend
function mirror_to_mirror_nostorage() {
    mirror2mirror imageset-config-full.yaml
    check_bundles localhost:${REGISTRY_DISCONN_PORT}/${CATALOGNAMESPACE}:test-catalog-latest \
    "bar.v0.1.0 bar.v0.2.0 bar.v1.0.0 baz.v1.0.0 baz.v1.0.1 baz.v1.1.0 foo.v0.1.0 foo.v0.2.0 foo.v0.3.0 foo.v0.3.1" \
    localhost:${REGISTRY_DISCONN_PORT}
}

# Test registry backend with custom namespace
function custom_namespace {
    run_full imageset-config-headsonly-backend-registry.yaml true "custom"
    check_bundles "localhost:${REGISTRY_DISCONN_PORT}/custom/${CATALOGNAMESPACE}:test-catalog-latest" \
    "bar.v0.1.0 bar.v0.2.0 bar.v1.0.0 baz.v1.1.0 foo.v0.3.1" \
    localhost:${REGISTRY_DISCONN_PORT} "custom"

    run_diff imageset-config-headsonly-backend-registry.yaml "custom"
    check_bundles "localhost:${REGISTRY_DISCONN_PORT}/custom/${CATALOGNAMESPACE}:test-catalog-latest" \
    "bar.v0.1.0 bar.v0.2.0 bar.v1.0.0 baz.v1.1.0 foo.v0.3.1 foo.v0.3.2" \
    localhost:${REGISTRY_DISCONN_PORT} "custom"
}