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

      # Extends home-manager's built-in services.clipse module with options
      # specific to this fork. Declares only the missing options, then uses
      # lib.mkForce to override the generated config.json so it includes both
      # the upstream fields and the fork-specific ones.
      homeManagerModules.default = { config, lib, pkgs, ... }:
        let
          cfg = config.services.clipse;
          jsonFormat = pkgs.formats.json { };
        in
        {
          options.services.clipse = {
            excludedApps = lib.mkOption {
              type = lib.types.listOf lib.types.str;
              default = [
                "1Password" "Bitwarden" "KeePassXC" "LastPass"
                "Dashlane" "Password Safe" "Keychain Access"
              ];
              description = "Applications excluded from clipboard history.";
            };

            enableMouse = lib.mkOption {
              type = lib.types.bool;
              default = true;
              description = "Enable mouse support in the TUI.";
            };

            enableDescription = lib.mkOption {
              type = lib.types.bool;
              default = true;
              description = "Show item descriptions in the TUI.";
            };

            syncPrimarySelection = lib.mkOption {
              type = lib.types.bool;
              default = false;
              description = "Sync clipboard with the X11 primary selection.";
            };

            deleteAfter = lib.mkOption {
              type = lib.types.int;
              default = 0;
              description = "Delete history entries after this many days (0 disables).";
            };

            pollInterval = lib.mkOption {
              type = lib.types.int;
              default = 50;
              description = "Clipboard polling interval in milliseconds.";
            };

            maxEntryLength = lib.mkOption {
              type = lib.types.int;
              default = 65;
              description = "Maximum display length for history entries in the TUI.";
            };

            autoPaste = {
              enabled = lib.mkOption {
                type = lib.types.bool;
                default = false;
                description = "Automatically paste the selected entry.";
              };
              keybind = lib.mkOption {
                type = lib.types.str;
                default = "ctrl+v";
                description = "Keybind used to trigger auto-paste.";
              };
              buffer = lib.mkOption {
                type = lib.types.int;
                default = 10;
                description = "Delay in milliseconds between selection and paste.";
              };
            };
          };

          config = lib.mkIf cfg.enable {
            # Override the config.json generated by home-manager's built-in module,
            # merging its fields with the fork-specific ones declared above.
            xdg.configFile."clipse/config.json".source = lib.mkForce (
              jsonFormat.generate "settings" {
                # Fields from the upstream home-manager module
                inherit (cfg) allowDuplicates imageDisplay keyBindings;
                historyFile = "clipboard_history.json";
                maxHistory = cfg.historySize;
                logFile = "clipse.log";
                themeFile = "custom_theme.json";
                tempDir = "tmp_files";

                # Fork-specific fields
                inherit (cfg)
                  excludedApps
                  enableMouse
                  enableDescription
                  syncPrimarySelection
                  deleteAfter
                  pollInterval
                  maxEntryLength;
                autoPaste = {
                  inherit (cfg.autoPaste) enabled keybind buffer;
                };
              }
            );
          };
        };
    };
}
