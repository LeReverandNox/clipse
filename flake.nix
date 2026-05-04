{
  description = "A clipboard manager for Linux (LeReverandNox fork)";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachSystem [ "x86_64-linux" "aarch64-linux" ] (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        packages.default = pkgs.buildGoModule {
          pname = "clipse";
          version = "1.2.1-LeReverandNox";
          src = self;

          vendorHash = "sha256-f9u9dGnfPE2v9WTHAlxGnPwioyJf1o0gSPyQcgORoR4=";
          proxyVendor = true;

          tags = [ "linux" ];

          nativeBuildInputs = [ pkgs.pkg-config ];

          buildInputs = with pkgs; [
            libx11
            libxfixes
            libxtst
            libxi
            libxinerama
            libxrandr
            libxcursor
            libxkbcommon
          ];

          postInstall = ''
            mkdir -p $out/lib/systemd/user
            cat > $out/lib/systemd/user/clipse.service << EOF
            [Unit]
            Description=Clipse
            After=network.target

            [Service]
            ExecStart=$out/bin/clipse -listen-x11
            Restart=on-failure

            [Install]
            WantedBy=default.target
            EOF
          '';

          meta = with pkgs.lib; {
            description = "A clipboard manager for Linux";
            homepage = "https://github.com/LeReverandNox/clipse";
            license = licenses.mit;
            mainProgram = "clipse";
            platforms = [ "x86_64-linux" "aarch64-linux" ];
          };
        };
      }
    ) // {
      overlays.default = final: prev: {
        clipse = self.packages.${final.stdenv.hostPlatform.system}.default;
      };
      nixosModules.default = { config, lib, pkgs, ... }:
        let cfg = config.services.clipse;
        in {
          options.services.clipse.enable = lib.mkEnableOption "Clipse clipboard manager";

          config = lib.mkIf cfg.enable {
            systemd.user.services.clipse = {
              description = "Clipse clipboard manager";
              after = [ "network.target" ];
              wantedBy = [ "default.target" ];
              serviceConfig = {
                ExecStart = "${self.packages.${pkgs.system}.default}/bin/clipse -listen-x11";
                Restart = "on-failure";
              };
            };
          };
        };
    };
}
