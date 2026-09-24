# Packaging expression for stellar, written to be copy-pasteable into nixpkgs
# as pkgs/by-name/st/stellar/package.nix.
#
# The flake in this repo builds the working tree instead (see flake.nix); this
# file is the release build, pinned to a tag. Both share the same vendorHash,
# so bumping dependencies means updating it in both places.
{
  lib,
  buildGoModule,
  fetchFromGitHub,
  installShellFiles,
  stdenv,
}:

buildGoModule (finalAttrs: {
  pname = "stellar";
  version = "1.6.0";

  src = fetchFromGitHub {
    owner = "a3chron";
    repo = "stellar";
    tag = "v${finalAttrs.version}";
    hash = "sha256-IxmTBLnAo3qmMIY2kzbWSG8W82RWDXmDM2T3jL3WohE=";
  };

  vendorHash = "sha256-fHvoSeRUqDVrKkk7q4wmRTdUXR2JsZRYh9ddHGEmOHs=";

  # Mirrors the ldflags in .goreleaser.yaml so `stellar version` reports the
  # real version rather than "dev" - which also matters behaviourally, since
  # the CLI skips its update check and telemetry entirely on dev builds.
  #
  # `date` is deliberately left at its "unknown" default: a build timestamp
  # would make the output differ between rebuilds of identical sources.
  #
  # No -X main.commit either, so `stellar version` shows "commit: none".
  # `finalAttrs.src.rev` is the tag ref, not a SHA, so the commit would have
  # to be a literal - and nixpkgs' update bot only bumps `version` and the
  # hashes, so a pinned SHA would silently go stale on the first automated bump.
  ldflags = [
    "-s"
    "-w"
    "-X main.version=${finalAttrs.version}"
  ];

  nativeBuildInputs = [ installShellFiles ];

  preCheck = ''
    # The test suite writes into ~/.config/stellar, and HOME is not writable
    # in the build sandbox.
    export HOME=$(mktemp -d)
    export STELLAR_NO_TELEMETRY=1
  '';

  postInstall = lib.optionalString (stdenv.buildPlatform.canExecute stdenv.hostPlatform) ''
    # `stellar completion` is an ordinary subcommand, so it runs the root
    # command's PersistentPreRunE: that creates ~/.config/stellar and kicks off
    # the anonymous install report. Give it a scratch HOME so it can't fail on
    # the read-only sandbox one, and opt out of telemetry - a package build has
    # no business reporting itself as an install.
    export HOME=$(mktemp -d)
    export STELLAR_NO_TELEMETRY=1

    installShellCompletion --cmd stellar \
      --bash <($out/bin/stellar completion bash) \
      --fish <($out/bin/stellar completion fish) \
      --zsh <($out/bin/stellar completion zsh)
  '';

  meta = {
    description = "Discover, preview, and apply Starship prompt themes";
    longDescription = ''
      stellar fetches Starship prompt themes from the stellar hub and applies
      them by pointing ~/.config/starship.toml at a cached copy, so switching
      between community themes and your own local configs is a single command.
      It keeps a versioned backup of any starship.toml it did not create.
    '';
    homepage = "https://github.com/a3chron/stellar";
    changelog = "https://github.com/a3chron/stellar/releases/tag/v${finalAttrs.version}";
    license = lib.licenses.mit;
    # TODO: add yourself here before opening the nixpkgs PR - it needs a
    # matching entry in maintainers/maintainer-list.nix.
    maintainers = [ ];
    mainProgram = "stellar";
    platforms = lib.platforms.unix ++ lib.platforms.windows;
  };
})
