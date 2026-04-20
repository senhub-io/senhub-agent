# Prompt Claude Code — Sortie Prometheus / VictoriaMetrics

À coller dans Claude Code à l'ouverture du repo `senhub-agent`.
Fichiers requis dans `docs/prometheus/` : `SPEC-prometheus-integration.md` et `prometheus-scrape-reference.yaml`.
Si la sortie Zabbix est déjà implémentée, l'audit Phase 0 existe déjà.

---

```
Tu vas implémenter la sortie Prometheus / VictoriaMetrics native du SenHub Agent.

## Documents de référence (à lire AVANT toute action)

1. docs/prometheus/SPEC-prometheus-integration.md — Spec fonctionnelle. §0, §5 et §6 CRITIQUES.
2. docs/prometheus/prometheus-scrape-reference.yaml — Config scrape de référence.
3. Si la sortie Zabbix existe : docs/zabbix/EXISTING-ARCHITECTURE.md (audit déjà fait).

## Terminologie

- Probe = type de collecteur (netscaler, citrix_cvad, vmware…)
- Instance de probe = une probe instanciée avec un nom unique dans la config
- Agent = le processus qui fait tourner N instances de probes
- Labels Prometheus : probe_name, probe_type (PAS instance_name — pour éviter la collision avec le label réservé "instance" de Prometheus)

## Contexte ABSOLU

- L'agent est en PRODUCTION. Zéro breaking change.
- L'agent a un BUS INTERNE. Des sorties (cloud, Zabbix potentiellement) le consomment.
- La sortie Prometheus = NOUVEAU CONSOMMATEUR du même bus.
- Route /metrics ajoutée au SERVEUR HTTP EXISTANT (même port, même auth).
- Si la sortie Zabbix existe et maintient un snapshot mémoire, la sortie Prometheus RÉUTILISE ce snapshot (pas de deuxième cache).
- Les probes et les sorties existantes ne bougent pas.
- Si la spec contredit le code, le code gagne.

## Workflow Git

1. git checkout -b feature/prometheus-export
   (ou continuer sur la branche Zabbix si les deux sont livrés ensemble — me demander)
2. Commits : feat(prometheus-export): ... / test(prometheus-export): ...
3. Pas de merge sans ma validation.

## Méthode par phases — fin de CHAQUE phase : commit, récap, attente du go.

# Phase 0 — AUDIT ou REVUE

## Si l'audit Zabbix existe déjà :

Produis docs/prometheus/IMPLEMENTATION-PLAN.md qui couvre :
- Réutilisation du snapshot existant (ou format pivot partagé)
- TABLE DE NOMMAGE COMPLÈTE : toutes les clés bus internes → noms Prometheus + type (gauge/counter). C'est LE point de validation. Je dois la valider avant toute ligne de code.
- Mapping des labels (probe_name, probe_type, group, subgroup, tags config)
- Gestion des métriques textuelles (filtrées ou converties)
- Choix client_golang/prometheus vs sérialisation manuelle
- Package cible
- Phases + critères de done
- Questions ouvertes (max 5)

## Si l'audit n'existe PAS :

Fais l'audit complet (identique au prompt Zabbix), PUIS le plan.

TU T'ARRÊTES et tu attends ma validation du plan (et surtout de la table de nommage).

# Phases d'implémentation

Phase 1 — Types + nommage + sérialisation
- Fonction transformation clé interne → nom Prometheus (spec §6)
- Sérialisation text exposition (HELP / TYPE / valeur{labels} )
- Labels : probe_name, probe_type, group, subgroup, tags config
- Filtrage métriques textuelles non convertibles
- Tests unitaires : nommage, sérialisation, filtrage, cas limites

Phase 2 — Branchement snapshot + handler HTTP
- Si snapshot partagé : s'y brancher, sérialiser à la volée au scrape
- Si pas de snapshot : créer consommateur bus + snapshot (même pattern que les autres sorties)
- Handler GET /metrics sur le routeur existant
- Auth : Bearer header + fallback query param ?token= (spec §7)
- Test intégration : bus mocké → GET /metrics → parsable par expfmt.TextParser

Phase 3 — Config + lifecycle + non-régression
- Section prometheus_export dans le YAML de conf (spec §8)
- Activation conditionnelle
- Wiring au démarrage (même endroit que les autres sorties)
- Test end-to-end : agent réel + curl /metrics + grep senhub_
- Vérif non-régression : sorties cloud et Zabbix vertes

Phase 4 — Validation réelle
- Scrape par vmagent ou Prometheus de test
- Métriques visibles dans VictoriaMetrics / Grafana, labels corrects, PromQL fonctionnel
- Je m'occupe de la conf vmagent, tu ajustes si besoin

Phase 5 — Documentation + CHANGELOG
- docs/prometheus-integration.md (guide utilisateur)
- docs/prometheus-metrics-reference.md (liste métriques, types, labels)
- Copie de prometheus-scrape-reference.yaml
- CHANGELOG

# Règles dures

- Rien hors-spec sans validation.
- Le NOMMAGE est contractuel. Une fois publié, ça ne change plus. Valide la table complète AVANT de coder.
- Pas de nouvelle dépendance Go sans justification. Sérialisation manuelle du text exposition acceptable et même préférable si client_golang n'est pas déjà dans le projet.
- Tests chaque fichier (couverture ≥ 80%).
- Body de GET /metrics parsable par expfmt.TextParser. Test automatique obligatoire.
- gofmt, go vet, linter verts à chaque commit.
- Labels stables et contractuels (probe_name, probe_type, group, subgroup). Pas de label inventé sans validation.
- CARDINALITÉ : labels à valeur dynamique INTERDITS. Seules valeurs variables autorisées : probe_name, probe_type, group, subgroup, tags config. Log warning au démarrage si un tag a une tête de valeur dynamique.

# Communication

- En français, concis
- La table de nommage dans le plan est LE point de validation critique. Pas de Phase 1 sans feu vert dessus.
- Préviens-moi de tout écart spec/conventions

Démarre par la Phase 0.
```
