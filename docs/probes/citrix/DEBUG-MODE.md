# Mode Debug - Extraction des identifiants Citrix

## Objectif
Le mode debug permet d'extraire et de comparer les identifiants des machines et sessions depuis les deux APIs Citrix :
- **CVAD REST API** (Delivery Controller)
- **OData API** (Director)

Cette analyse est nécessaire pour implémenter le filtrage CVAD→OData et s'assurer que nous récupérons uniquement les métriques des machines du site configuré.

## Configuration

### 1. Fichier de configuration
Utilisez l'exemple `citrix-debug-example.yaml` ou ajoutez à votre configuration existante :

```yaml
# probes.d/10-citrix.yaml — each file under probes.d/ is a YAML array of probes
- name: citrix
  type: citrix
  params:
    base_url: "https://your-director.company.com"
    debug_identifiers: true  # ACTIVE LE MODE DEBUG

    auth:
      username: "DOMAIN\\username"
      password: ${secret:citrix.password}   # auto-sealed on install

    delivery_controller:
      url: "https://your-ddc.company.com"
      site_filter: "YourSiteName"
```

### 2. Lancement
```bash
# Avec configuration spécifique
./agent run --config citrix-debug-example.yaml

# Ou avec votre config existante (si debug_identifiers: true)
./agent run --config your-config.yaml
```

## Fichiers générés

Le mode debug génère des fichiers JSON dans `/tmp/citrix-debug/` :

- `cvad_identifiers_SITENAME_TIMESTAMP.json` - Données CVAD REST API
- `odata_identifiers_SITENAME_TIMESTAMP.json` - Données OData Director API

### Structure des fichiers
```json
{
  "extraction_time": "2024-09-10T15:30:00Z",
  "site_name": "Production",
  "api_type": "CVAD_REST",
  "machines_count": 259,
  "sessions_count": 45,
  "machines": [...],  // Array des machines avec tous leurs identifiants
  "sessions": [...]   // Array des sessions avec tous leurs identifiants
}
```

## Analyse des correspondances

### Identifiants machines
**CVAD (DDCMachine):**
- `Id` : GUID unique Citrix
- `Name` : Nom NetBIOS 
- `DNSName` : FQDN (ex: server.domain.com)
- `MachineName` : Nom machine

**OData (Machine):**
- `Id` : GUID unique Citrix  
- `Name` : Nom NetBIOS
- `DnsName` : FQDN (ex: server.domain.com)
- `NetBiosName` : Nom NetBIOS

### Commandes d'analyse
```bash
# Voir les premiers éléments de chaque fichier
cat /tmp/citrix-debug/cvad_*.json | jq '.machines[0]'
cat /tmp/citrix-debug/odata_*.json | jq '.machines[0]'

# Compter les machines dans chaque API
cat /tmp/citrix-debug/cvad_*.json | jq '.machines_count'
cat /tmp/citrix-debug/odata_*.json | jq '.machines_count'

# Extraire uniquement les noms DNS
cat /tmp/citrix-debug/cvad_*.json | jq '.machines[].DNSName'
cat /tmp/citrix-debug/odata_*.json | jq '.machines[].DnsName'
```

## Correspondances attendues
- `CVAD.DNSName` ↔ `OData.DnsName`
- `CVAD.Id` ↔ `OData.Id` (même GUID)
- `CVAD.Name` ↔ `OData.Name` ou `OData.NetBiosName`

## Étapes suivantes
1. Analyser les correspondances manuellement
2. Identifier le champ le plus fiable pour le filtrage
3. Implémenter le système de filtrage basé sur les machines CVAD
4. Tester les performances avec filtrage vs sans filtrage

## Désactiver le mode debug
Remettez `debug_identifiers: false` dans votre configuration pour revenir au mode normal de collecte de métriques.