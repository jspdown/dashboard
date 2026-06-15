# Lint checks (`nix flake check`).
{
  perSystem =
    {
      pkgs,
      lib,
      goEnv,
      ...
    }:
    let
      golangciLint =
        name: src: env:
        pkgs.stdenv.mkDerivation {
          inherit name src;
          nativeBuildInputs = [
            pkgs.go
            pkgs.golangci-lint
            env.hooks.goConfigHook
          ];
          goVendorDir = env.vendorEnv;
          goCacheDir = env.goCacheEnv;
          CGO_ENABLED = 0;
          buildPhase = ''
            export HOME=$TMPDIR
            export GOLANGCI_LINT_CACHE=$TMPDIR/golangci-cache
            golangci-lint run ./...
          '';
          installPhase = "touch $out";
        };

      appNodeModules = pkgs.importNpmLock.buildNodeModules {
        npmRoot = ../app;
        inherit (pkgs) nodejs;
        derivationArgs.npmFlags = [ "--legacy-peer-deps" ];
      };

      shellSrc = lib.fileset.toSource {
        root = ../.;
        fileset = lib.fileset.fileFilter (file: file.hasExt "sh") ../.;
      };
    in
    {
      checks = {
        lint-api = golangciLint "lint-api" ../api (goEnv ../api ../api/gomod2nix.toml);
        lint-e2e = golangciLint "lint-e2e" ../e2e (goEnv ../e2e ../e2e/gomod2nix.toml);

        lint-app = pkgs.runCommandLocal "lint-app" { nativeBuildInputs = [ pkgs.nodejs ]; } ''
          export HOME=$TMPDIR
          cp -r --no-preserve=mode,ownership ${../app} app
          ln -s ${appNodeModules}/node_modules app/node_modules
          cd app
          npm run lint
          touch $out
        '';

        lint-shell =
          pkgs.runCommandLocal "lint-shell"
            {
              nativeBuildInputs = [
                pkgs.shellcheck
                pkgs.shfmt
              ];
            }
            ''
              cd ${shellSrc}
              shfmt -f . | xargs -r shellcheck
              shfmt -f . | xargs -r shfmt -d -i 4 -ci
              touch $out
            '';
      };
    };
}
