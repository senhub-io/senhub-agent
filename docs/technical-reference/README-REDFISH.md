# Test du Redfish Probe avec Postman

J'ai créé une collection Postman pour tester l'API Redfish. Cette collection vous permettra de vérifier les différentes parties de l'API qui sont utilisées par le probe Redfish.

## Prérequis

1. [Postman](https://www.postman.com/downloads/) installé sur votre machine
2. Un serveur avec une API Redfish accessible (comme votre Dell EMC)

## Configuration

1. Importez le fichier `redfish_postman_collection.json` dans Postman
2. Dans la collection, modifiez les variables suivantes:
   - `hostname` : l'adresse de votre serveur (par exemple `lb-me5024mgmt1.batistyl.fr`)
   - `username` : votre nom d'utilisateur pour l'authentification
   - `password` : votre mot de passe

## Tests recommandés

Exécutez les requêtes dans l'ordre pour tester les différentes parties de l'API:

1. **Get Service Root** - Vérifie la version de l'API Redfish
2. **Create Session** - Crée une session (stocke le token automatiquement)
3. **Get Systems Collection** - Liste les systèmes disponibles
4. **Get Managers Collection** - Vérifiez les différents chemins de l'iDRAC
   - Notez quels chemins sont disponibles et lesquels génèrent des 404
   - Vérifiez si `iDRAC.Embedded.1`, `iDRAC.Embedded`, `iDRAC` ou `1` sont accessibles
5. **Get Thermal Info** - Vérifie les données thermiques
6. **Get Power Info** - Vérifie les données d'alimentation
7. **Get Storage Collection** - Vérifie les contrôleurs de stockage
8. **Get Network Interfaces** - Vérifie les interfaces réseau

## Dépannage

Si certaines requêtes échouent:

1. Vérifiez que l'authentification fonctionne (Basic Auth ou Session)
2. Pour les chemin iDRAC, notez lesquels sont valides - cela aidera à comprendre comment votre serveur expose l'API
3. Les chemins peuvent varier selon les versions de l'API Redfish et du firmware iDRAC

## Utilisation avec la sonde Redfish

Le agent Redfish peut être configuré avec n'importe quel chemin d'API valide découvert via Postman. Si le mode de découverte automatique ne fonctionne pas, vous pouvez utiliser Postman pour identifier les chemins corrects à utiliser.