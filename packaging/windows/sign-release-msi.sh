#!/usr/bin/env bash
#
# Authenticode-sign a SenHub MSI with the Certum SimplySign cloud certificate.
#
# Signing model (see docs): the private key lives in Certum's HSM and is reached
# through the SimplySign PKCS#11 module. Authorisation is the OPEN SimplySign
# Desktop session on this machine (login + OTP, 2h window) — there is NO card PIN
# for this Code Signing token, so jsign is invoked without --storepass.
#
# The script is defensive on purpose: every precondition is checked and reported
# before touching the artifact, and the signature is verified after signing.
#
#   usage:  sign-release-msi.sh <file.msi> [--in-place]
#
# Without --in-place a signed COPY (<name>-signed.msi) is produced and the input
# is left untouched. With --in-place the input file itself is signed.
#
# Overridable via env: JSIGN_VERSION JSIGN_SHA256 PKCS11_MODULE TSA_URL
#                      SIGN_NAME SIGN_URL JSIGN_JAR JAVA17_HOME
#
set -euo pipefail

JSIGN_VERSION="${JSIGN_VERSION:-7.4}"
JSIGN_SHA256="${JSIGN_SHA256:-2abf2ade9ea322acc2d60c24794eadc465ff9380938fca4c932d09e0b25f1c28}"
PKCS11_MODULE="${PKCS11_MODULE:-/usr/local/lib/libSimplySignPKCS.dylib}"
TSA_URL="${TSA_URL:-http://time.certum.pl}"
SIGN_NAME="${SIGN_NAME:-SenHub Agent}"
SIGN_URL="${SIGN_URL:-https://senhub.io}"
CACHE_DIR="${XDG_CACHE_HOME:-$HOME/.cache}/senhub-codesign"

die()  { echo "ERROR: $*" >&2; exit 1; }
info() { echo ">> $*"; }
ok()   { echo "OK   $*"; }

MSI=""
IN_PLACE=0
for arg in "$@"; do
  case "$arg" in
    --in-place) IN_PLACE=1 ;;
    -*) die "unknown option: $arg" ;;
    *)  MSI="$arg" ;;
  esac
done
[ -n "$MSI" ]     || die "usage: sign-release-msi.sh <file.msi> [--in-place]"
[ -f "$MSI" ]     || die "file not found: $MSI"
case "$MSI" in *.msi) ;; *) die "expected a .msi file: $MSI" ;; esac

# --- 1. Java <=17 (jsign+PKCS#11 breaks on newer JDKs on macOS) --------------
JAVA_BIN=""
if [ -n "${JAVA17_HOME:-}" ] && [ -x "$JAVA17_HOME/bin/java" ]; then
  JAVA_BIN="$JAVA17_HOME/bin/java"
elif [ -x /usr/libexec/java_home ]; then
  if JH="$(/usr/libexec/java_home -v 17 2>/dev/null)"; then JAVA_BIN="$JH/bin/java"; fi
fi
[ -n "$JAVA_BIN" ] || die "Java 17 not found. Install Temurin 17 or set JAVA17_HOME. (jsign+PKCS#11 needs Java <=17 on macOS)"
ok "Java 17: $JAVA_BIN"

# --- 2. jsign jar (cached + digest-pinned) -----------------------------------
if [ -n "${JSIGN_JAR:-}" ]; then
  [ -f "$JSIGN_JAR" ] || die "JSIGN_JAR set but not found: $JSIGN_JAR"
else
  JSIGN_JAR="$CACHE_DIR/jsign-$JSIGN_VERSION.jar"
  if [ ! -f "$JSIGN_JAR" ]; then
    mkdir -p "$CACHE_DIR"
    info "fetching jsign $JSIGN_VERSION"
    curl -fsSL "https://repo1.maven.org/maven2/net/jsign/jsign/$JSIGN_VERSION/jsign-$JSIGN_VERSION.jar" -o "$JSIGN_JAR" \
      || die "download of jsign failed"
  fi
fi
ACTUAL="$(shasum -a 256 "$JSIGN_JAR" | awk '{print $1}')"
[ "$ACTUAL" = "$JSIGN_SHA256" ] || die "jsign digest mismatch: got $ACTUAL expected $JSIGN_SHA256"
ok "jsign $JSIGN_VERSION digest pinned"

# --- 3. SimplySign session (PKCS#11 module + reachable token) ----------------
[ -f "$PKCS11_MODULE" ] || die "PKCS#11 module missing: $PKCS11_MODULE — open SimplySign Desktop and log in (session)."
PKCS11_TOOL="$(command -v pkcs11-tool || true)"
ALIAS="${ALIAS:-}"
if [ -n "$PKCS11_TOOL" ]; then
  # read-only listing: never logs in, never consumes a PIN attempt
  SLOTS="$("$PKCS11_TOOL" --module "$PKCS11_MODULE" --list-token-slots 2>/dev/null || true)"
  echo "$SLOTS" | grep -q "token" || die "no SimplySign token visible — is the Desktop session open?"
  ok "SimplySign token present"
  if [ -z "$ALIAS" ]; then
    ALIAS="$("$PKCS11_TOOL" --module "$PKCS11_MODULE" --list-objects 2>/dev/null \
              | awk '/Certificate Object/{c=1} c&&/label:/{sub(/^[[:space:]]*label:[[:space:]]*/,""); print; exit}')"
    [ -n "$ALIAS" ] || die "could not auto-discover certificate alias — pass ALIAS=<label> explicitly"
  fi
  ok "certificate alias: $ALIAS"
else
  [ -n "$ALIAS" ] || die "pkcs11-tool (OpenSC) not found and no ALIAS given. brew install opensc, or set ALIAS=<cert label>."
  info "pkcs11-tool absent; using provided ALIAS=$ALIAS (token not pre-checked)"
fi

# --- 4. PKCS#11 config for jsign (temp, cleaned up) --------------------------
CFG="$(mktemp -t simplysign.XXXXXX.cfg)"
trap 'rm -f "$CFG"' EXIT
printf 'name = SimplySign\nlibrary = %s\nslotListIndex = 0\n' "$PKCS11_MODULE" > "$CFG"

# --- 5. sign ------------------------------------------------------------------
if [ "$IN_PLACE" = "1" ]; then OUT="$MSI"; else OUT="${MSI%.msi}-signed.msi"; cp -f "$MSI" "$OUT"; fi
info "signing: $OUT"
"$JAVA_BIN" -jar "$JSIGN_JAR" \
  --storetype PKCS11 \
  --keystore "$CFG" \
  --alias "$ALIAS" \
  --tsaurl "$TSA_URL" --tsmode rfc3161 \
  --name "$SIGN_NAME" --url "$SIGN_URL" \
  "$OUT"

# --- 6. verify (local structural + digest check) -----------------------------
if command -v osslsigncode >/dev/null 2>&1; then
  V="$(osslsigncode verify "$OUT" 2>/dev/null || true)"
  CUR="$(echo "$V" | awk -F': ' '/Current DigitalSignature/{gsub(/ /,"",$2);print $2}')"
  CAL="$(echo "$V" | awk -F': ' '/Calculated DigitalSignature/{gsub(/ /,"",$2);print $2}')"
  [ -n "$CUR" ] && [ "$CUR" = "$CAL" ] || die "signature digest mismatch after signing — signature NOT trustworthy"
  SUBJ="$(echo "$V" | awk -F': ' '/^[[:space:]]*Subject:/{print $2; exit}')"
  ok "signature digest verified (embedded == computed)"
  ok "signer: ${SUBJ:-<subject line not parsed>}"
  echo "$V" | grep -q "Timestamp Server Signature verification: ok" && ok "RFC3161 timestamp verified"
  echo
  echo "NOTE: osslsigncode may report 'unable to get local issuer certificate' on macOS —"
  echo "      that is only the Certum root missing from the OS trust store, NOT a signing"
  echo "      defect. The authoritative check is Windows Get-AuthenticodeSignature (Status=Valid)."
else
  info "osslsigncode not installed — skipping local verify (brew install osslsigncode to enable)"
fi

echo
ok "DONE -> $OUT"
