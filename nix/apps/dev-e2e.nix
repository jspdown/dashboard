# `nix run .#dev-e2e` — long-lived e2e stack behind Vite with HMR.
#
# Boots the same harness the e2e tests use (fake GitHub pre-seeded with the
# demo scenario, testcontainers Postgres, in-process api) and fronts it with
# Vite serving the live working-tree frontend source, so edits under app/
# hot-reload against realistic data. The harness stamps X-Forwarded-User
# server-side, so there is no sign-in step. This is the loop for autonomous
# UI iteration: run it in the background, edit app/src, screenshot the Vite
# URL, repeat.
#
# Vite resolves dependencies from app/node_modules; if the working tree has
# none, the Nix-built set (from the same package-lock.json the `app` package
# builds with) is symlinked in, so no npm install step is needed.
#
# Env overrides: DEV_E2E_ADDR (api stack, default 127.0.0.1:18080),
# VITE_HOST / VITE_PORT (frontend, default 127.0.0.1:5273).
{
  perSystem =
    { pkgs, lib, ... }:
    let
      appNodeModules = pkgs.importNpmLock.buildNodeModules {
        npmRoot = ../../app;
        inherit (pkgs) nodejs;
        derivationArgs.npmFlags = [ "--legacy-peer-deps" ];
      };
    in
    {
      apps.dev-e2e = {
        type = "app";
        meta.description = "Long-lived e2e stack + Vite HMR proxy";
        program = lib.getExe (
          pkgs.writeShellApplication {
            name = "dev-e2e";
            runtimeInputs = [
              pkgs.go
              pkgs.nodejs
              pkgs.git
              pkgs.curl
            ];
            text = ''
              root=$(git rev-parse --show-toplevel)
              # Use the pinned Go's own toolchain and stdlib. A shell that has
              # run `go` before may export GOROOT (and GOTOOLCHAIN auto can
              # pull a newer toolchain), which makes the pinned go binary load
              # a mismatched stdlib.
              export GOTOOLCHAIN=local
              unset GOROOT

              DEV_E2E_ADDR="''${DEV_E2E_ADDR:-127.0.0.1:18080}"
              VITE_HOST="''${VITE_HOST:-127.0.0.1}"
              VITE_PORT="''${VITE_PORT:-5273}"

              if [ ! -e "$root/app/node_modules" ]; then
                ln -s ${appNodeModules}/node_modules "$root/app/node_modules"
              fi

              trap 'kill "$STACK_PID" 2>/dev/null || true' EXIT INT TERM
              ( cd "$root/e2e" && go run ./cmd/dev-e2e -addr "$DEV_E2E_ADDR" ) &
              STACK_PID=$!
              until curl -sf "http://$DEV_E2E_ADDR/api/me" >/dev/null; do
                kill -0 "$STACK_PID" 2>/dev/null || exit 1
                sleep 0.1
              done

              cd "$root/app"
              DASHBOARD_API_URL="http://$DEV_E2E_ADDR" npm run dev -- \
                --host "$VITE_HOST" --port "$VITE_PORT" --strictPort
            '';
          }
        );
      };
    };
}
