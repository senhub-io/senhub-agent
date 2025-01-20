#!/bin/bash

# Fonctions de logging avec couleurs (adaptées pour CI)
log_info() {
    echo "::info::$1"
}

log_success() {
    echo "::notice::$1"
}

log_error() {
    echo "::error::$1"
}

log_warn() {
    echo "::warning::$1"
}

# Configuration
GITHUB_REPO="senhub-io/senhubagent"
GITHUB_TOKEN="${GITHUB_TOKEN}"
S3_BUCKET="s3://senhub-agent"
TEMP_DIR="/tmp/releases"
S3_ENDPOINT="https://s3.rbx.io.cloud.ovh.net"

# Vérification des variables d'environnement
for var in GITHUB_TOKEN S3_ACCESS_KEY S3_SECRET_KEY; do
    if [ -z "${!var}" ]; then
        log_error "La variable $var n'est pas définie"
        exit 1
    fi
done

# Début du script
log_info "Démarrage de la synchronisation GitHub -> S3"
log_info "Repo: $GITHUB_REPO"
log_info "Bucket: $S3_BUCKET"

# Création du dossier temporaire
rm -rf $TEMP_DIR
mkdir -p $TEMP_DIR || {
    log_error "Impossible de créer le dossier temporaire"
    exit 1
}

# Récupération des informations de la release
releases_data=$(curl -s -H "Authorization: token $GITHUB_TOKEN" \
     -H "Accept: application/vnd.github.v3+json" \
     "https://api.github.com/repos/$GITHUB_REPO/releases/latest")

tag_name=$(echo $releases_data | jq -r '.tag_name')
log_info "Version trouvée: $tag_name"

# Pour chaque asset
echo $releases_data | jq -r '.assets[] | select(.state == "uploaded") | "\(.url) \(.name) \(.size)"' | while read -r asset_url name size; do
    log_info "Téléchargement de $name"

    # Téléchargement de l'asset
    curl -L \
         -H "Accept: application/octet-stream" \
         -H "Authorization: token $GITHUB_TOKEN" \
         -H "X-GitHub-Api-Version: 2022-11-28" \
         "$asset_url" \
         --output "$TEMP_DIR/$name"

    # Size check
    actual_size=$(stat --format=%s "$TEMP_DIR/$name")

    if [ "$actual_size" -eq "$size" ]; then
        log_success "Téléchargement réussi pour $name"

        # Upload vers S3
        if s3cmd put "$TEMP_DIR/$name" "$S3_BUCKET/$tag_name/$name" --host=$S3_ENDPOINT; then
            log_success "Upload réussi pour $name"

            # Mise à jour du lien latest
            if s3cmd cp "$S3_BUCKET/$tag_name/$name" "$S3_BUCKET/latest/$name" --host=$S3_ENDPOINT; then
                log_success "Lien latest mis à jour pour $name"
            else
                log_error "Erreur lors de la mise à jour latest pour $name"
                exit 1
            fi
        else
            log_error "Erreur lors de l'upload de $name"
            exit 1
        fi
    else
        log_error "Taille incorrecte pour $name ($actual_size vs $size bytes attendus)"
        exit 1
    fi
done

# Nettoyage
rm -rf $TEMP_DIR
log_success "Synchronisation terminée avec succès"
