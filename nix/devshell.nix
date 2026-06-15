# Developer shell (`nix develop`).
{
  perSystem =
    { pkgs, ... }:
    {
      devShells.default = pkgs.mkShell {
        packages = [
          pkgs.go
          pkgs.nodejs
          pkgs.golangci-lint
          pkgs.shellcheck
          pkgs.shfmt
        ];
      };
    };
}
