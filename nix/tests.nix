# Test checks (`nix flake check`).
{
  perSystem =
    { pkgs, goEnv, ... }:
    let
      apiEnv = goEnv ../api ../api/gomod2nix.toml;
    in
    {
      checks.test-unit = pkgs.stdenv.mkDerivation {
        name = "test-unit";
        src = ../api;
        nativeBuildInputs = [
          pkgs.go
          apiEnv.hooks.goConfigHook
        ];
        goVendorDir = apiEnv.vendorEnv;
        goCacheDir = apiEnv.goCacheEnv;
        CGO_ENABLED = 0;
        buildPhase = ''
          export HOME=$TMPDIR
          go test ./pkg/... ./cmd/...
        '';
        installPhase = "touch $out";
      };
    };
}
