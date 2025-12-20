{
  description = "A development environment with Taskfile";

  inputs = {
    # Pinning a specific version of nixpkgs for reproducibility
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.05";
  };

  outputs = { self, nixpkgs, ... }: 
    let
      # Define the systems we want to support
      supportedSystems = [ "x86_64-linux" "aarch64-darwin" "x86_64-darwin" ];

      # Helper function to generate outputs for all supported systems
      forAllSystems = nixpkgs.lib.genAttrs supportedSystems;
    in
    {
      # Define development shells
      devShells = forAllSystems (system:
        let
          pkgs = import nixpkgs { inherit system; };
        in
        {
          default = pkgs.mkShell {
            # List packages you want available in the shell's PATH
            packages = with pkgs; [
              go-task  # https://taskfile.dev/
              go
              git
              goreleaser
              bash
            ];

          };
        }
      );
    };
}
