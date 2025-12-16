# Session: Amélioration des noms PRTG pour Netscaler + Fork SDK
**Date**: 2025-12-10 → 2025-12-11
**Statut**: ✅ Terminé - Binaire prêt avec fix SDK

## Problème résolu

Les métriques Netscaler dans PRTG affichaient des noms dupliqués sans distinction :
```json
{
  "channel": "Netscaler Lbvserver State",
  "value": 1
}
```

Impossible de distinguer quel VServer, Service ou ServiceGroup était concerné.

## Solution implémentée

### Fichier modifié
`internal/agent/services/data_store/transformers/definitions/netscaler.yaml`

### Changements effectués

1. **Ajout de `multi_instance_labels`** au niveau probe :
   ```yaml
   multi_instance_labels: ["vserver", "service", "servicegroup"]
   ```

2. **Mise à jour de tous les `display_name`** pour inclure les tags discriminants :
   - `"VServer State"` → `"VServer State ({vserver})"`
   - `"Service State"` → `"Service State ({service})"`
   - `"Service Group State"` → `"Service Group State ({servicegroup})"`

3. **Ajout de `multi_instance_labels`** pour chaque métrique correspondante

### Résultat attendu

```json
{
  "channel": "VServer State (VS_LB_PROD_SF_HTTPS_443)",
  "value": 1
},
{
  "channel": "VServer State (VS_LB_PROD_SF_HTTP_80)",
  "value": 7
},
{
  "channel": "Service State (SVC_PROD_WEB_01)",
  "value": 1
},
{
  "channel": "Service Group State (SG_PROD_BACKEND)",
  "value": 1
}
```

## Changements additionnels (2025-12-11)

### ⚠️ Fork temporaire du SDK Citrix

Pour corriger le bug des ressources singleton (system, ns, ssl), nous utilisons temporairement un fork :

**Fork**: https://github.com/senhub-io/adc-nitro-go
**Branch**: `fix/singleton-stats-support`
**Commit**: `ee923d74da8155d8caec51efdd3739116cb62f81`

**Documentation complète**: `docs/.internal/TEMPORARY-FORK-citrix-adc-nitro-go.md`

### Métriques réactivées

✅ System CPU/Memory (8 métriques)
✅ NS Global Throughput (2 métriques)
✅ SSL Transactions/Sessions (2 métriques)
✅ LB VServer (5 métriques par VServer)
✅ Services (3 métriques par Service)
✅ ServiceGroups (3 métriques par ServiceGroup)

### Binary final

✅ **Windows binary**: `dist/senhub-agent_windows_amd64.exe` (19M)
- Compilé avec le fork SDK fixé
- Toutes les métriques activées (system, ns, ssl, lbvserver, service, servicegroup)
- Noms PRTG différenciés avec tags discriminants
- Prêt à être déployé sur le serveur Windows

## Tests à effectuer

1. Déployer le nouveau binary Windows sur le serveur
2. Relancer l'agent avec la config Netscaler
3. Vérifier l'endpoint PRTG : `http://localhost:8080/api/{key}/prtg/metrics/netscaler-adc`
4. Confirmer que les channel names incluent maintenant les valeurs entre parenthèses

## Configuration utilisée

```yaml
probes:
  - name: netscaler-adc
    type: netscaler
    params:
      base_url: "https://SRV0006.noble-age.fr"
      username: "nsroot"
      password: "***"
      interval: 60
      insecure_skip_verify: true
```

## Métriques collectées

- **LB VServer**: state, requests.rate, connections.current, throughput.rx/tx
- **Services**: state, throughput, transactions.active
- **Service Groups**: state, requests.rate, members.active

**Note**: Les métriques système/NS/SSL sont désactivées en raison d'un bug dans la librairie Citrix (`adc-nitro-go`)

## Contexte technique

### Tags discriminants enregistrés
Fichier: `internal/agent/services/data_store/strategies/http/http_cache.go`
```go
"netscaler": {"vserver", "service", "servicegroup"}
```

### Fonctionnement du transformer
1. Le transformer charge `definitions/netscaler.yaml`
2. Pour chaque métrique, il trouve la définition correspondante
3. Il remplace les placeholders `{vserver}`, `{service}`, `{servicegroup}` par les valeurs des tags
4. Le résultat est utilisé comme channel name dans PRTG

## Commits à faire

```bash
git add internal/agent/services/data_store/transformers/definitions/netscaler.yaml
git commit -m "fix(netscaler): add discriminant tags to PRTG channel names

- Add multi_instance_labels for vserver, service, servicegroup
- Include tag values in display_name (e.g., 'VServer State ({vserver})')
- Remove 'Netscaler' prefix from metric names
- Makes PRTG output more readable and distinguishable"
```

## Prochaines étapes possibles

1. ✅ Tester le binary Windows avec SRV0006
2. ⏳ Potentiellement réactiver les métriques système/NS/SSL si le bug Citrix est corrigé
3. ⏳ Ajouter d'autres métriques Netscaler si nécessaire (certificats SSL, cache, etc.)

---

**Status des fichiers modifiés**:
- ✅ `netscaler.yaml` - Définitions mises à jour
- ✅ `senhub-agent_windows_amd64.exe` - Binary recompilé
- ⏳ Tests en production

**Prêt pour le déploiement!**
