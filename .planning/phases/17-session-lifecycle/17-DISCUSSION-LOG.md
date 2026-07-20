# Phase 17: Session Lifecycle - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-20
**Phase:** 17-Session Lifecycle
**Areas discussed:** Rétention post-stop, Arguments de start_session, Forme du résumé de stop, Format du session_id

---

## Rétention post-stop (tension SESS-06 ↔ QRY-04)

### Q1 — Quand le tmpdir de session est-il réellement supprimé ?

| Option | Description | Selected |
|--------|-------------|----------|
| Retenu après stop | stop_session finalise et retient le tmpdir; état "stopped" interrogeable; suppression au prochain start_session ou au shutdown | ✓ |
| Teardown complet à stop | PROJECT.md littéral; tout dans le résumé final; QRY-04 à réécrire | |
| Rétention TTL | Purge par timer après N minutes; goroutine de purge à tester | |

**User's choice:** Retenu après stop (Recommended)
**Notes:** Résout la tension en faveur du workflow LLM naturel capture → stop → analyse. Supersède la formulation « tmpdir cleaned at stop_session » de PROJECT.md.

### Q2 — stop_session sur session déjà stoppée : idempotent ou erreur ?

| Option | Description | Selected |
|--------|-------------|----------|
| Idempotent | Deuxième stop → même résumé final + marqueur "already stopped"; LLM-retry friendly | ✓ |
| Erreur isError | Sémantique stricte de handle; un retry innocent devient un échec visible | |

**User's choice:** Idempotent (Recommended)

### Q3 — start_session alors qu'une session stoppée est retenue ?

| Option | Description | Selected |
|--------|-------------|----------|
| Purge silencieuse + note | Nouveau start réussit, supprime l'ancien tmpdir, réponse note "previous session sess_X discarded" | ✓ |
| Refus tant que non purgée | Exige un discard explicite; friction et état à gérer pour le LLM | |

**User's choice:** Purge silencieuse + note (Recommended)

---

## Arguments de start_session

### Q1 — Quelle surface d'arguments ?

| Option | Description | Selected |
|--------|-------------|----------|
| Curatée | namespace/all_namespaces + l7 + ignore_drop_reasons + ignore_protocols | ✓ (base) |
| Minimale | namespace/all_namespaces uniquement | |
| Quasi-parité | Tout sauf les non-sens MCP (~10 champs) | |

**User's choice:** Curatée (Recommended)

### Q2 — Args limites à ajouter ? (multi-sélection)

| Option | Description | Selected |
|--------|-------------|----------|
| server | Bypass port-forward; rend le e2e SRV-04 testable sans kubeconfig | ✓ |
| cluster_dedup | Bool défaut false; readonly (list CNP); latence au start + RBAC | ✓ |
| flush_interval | Cadence d'écriture des artefacts | ✓ |
| Aucun ajout | Surface strictement curatée | |

**User's choice:** Les trois args ajoutés
**Notes:** Surface finale ≈ quasi-parité moins les evidence caps — choix cohérent de l'utilisateur après avoir vu chaque arg individuellement.

### Q3 — server exposé : tls/timeout aussi ?

| Option | Description | Selected |
|--------|-------------|----------|
| tls + timeout aussi | Cohérence CLI; timeout borne la connexion → échec rapide en isError | ✓ |
| server seul | tls figé false, timeout figé 10s | |

**User's choice:** tls + timeout aussi (Recommended)

---

## Forme du résumé de stop

### Q1 — Comment stop_session obtient-il les SessionStats complètes ?

| Option | Description | Selected |
|--------|-------------|----------|
| Hook additif nil-safe | Champ optionnel sur PipelineConfig (OnFinal func(SessionStats)), une fois après g.Wait(); nil = no-op; pas LIVE-01 | ✓ |
| Read-back disque pur | RunPipeline strictement inchangé; résumé partiel (policies skipped/failed, lost events, L7 invisibles) | |

**User's choice:** Hook additif nil-safe (Recommended)
**Notes:** Découverte technique en séance : cluster-health.json ne persiste que flows_seen/infra_drops_total/started/ended — les autres compteurs SessionStats sont irrécupérables depuis le disque.

### Q2 — Forme du résultat de stop_session ?

| Option | Description | Selected |
|--------|-------------|----------|
| Structuré seul | structuredContent typé; bloc humain reste sur stderr via mcpModeStdout() — handoff Phase 16 intact | ✓ |
| Structuré + bloc humain | Buffer par session + texte en content; double sérialisation, coût tokens | |

**User's choice:** Structuré seul (Recommended)

---

## Format du session_id

### Q1 — Format du session_id MCP ?

| Option | Description | Selected |
|--------|-------------|----------|
| sess_<uuid> distinct | ID MCP opaque mappé par le Manager; evidence SessionID interne inchangé; log zap corrèle les deux | ✓ |
| Une seule valeur partagée | session_id == PipelineConfig.SessionID (RFC3339-uuid4); grep-able mais pas opaque | |

**User's choice:** sess_<uuid> distinct (Recommended)

---

## Claude's Discretion

- Valeurs exactes des deadlines de cleanup SESS-05 (bornées par étape)
- Sémantique des comptages de fichiers de get_status et noms de champs de réponse
- Layout de pkg/session et forme de l'API du Manager (Pattern 1 research en référence)
- Textes d'erreur port-forward/kubeconfig à start_session (distincts, actionnables)
- Noms de champs du struct de résumé de stop (cohérents avec la discipline QRY-05 de Phase 18)

## Deferred Ideas

Aucune — la discussion est restée dans le périmètre de la phase. (LIVE-01, FLOW-01, REDACT-01 étaient déjà tracées en v2 avant cette discussion.)
