# Phase 16: MCP Server Foundation & Write Safety - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-20
**Phase:** 16-MCP Server Foundation & Write Safety
**Areas discussed:** Blindage stdout, Périmètre test pureté

---

## Blindage stdout

### Q1 — Stratégie d'application de la pureté stdout

| Option | Description | Selected |
|--------|-------------|----------|
| Câblage + swap global (Recommandé) | SRV-02 (câblage explicite + test) ET, au démarrage de cpg mcp : le transport SDK capture le vrai os.Stdout d'abord, puis os.Stdout = os.Stderr. Tout print parasite futur atterrit visible dans stderr au lieu de corrompre le wire. ~5 lignes, élimine la classe de bug « chief risk » de la recherche. | ✓ |
| Câblage explicite seul | Strictement SRV-02 : chaque seam wiré explicitement, le test de pureté fait foi. Pas de magie globale — un print parasite non couvert par un test corromprait le wire en prod. | |

**User's choice:** Câblage + swap global

### Q2 — Destination des seams « sortie humaine » en mode MCP

| Option | Description | Selected |
|--------|-------------|----------|
| stderr (Recommandé) | Résumé de session visible dans les logs harness — rien n'est perdu, cohérent avec le swap global. Ph17 libre de capturer ce seam dans un buffer pour stop_session. diffOut ne tourne jamais en MCP mais wiré par principe. | ✓ |
| io.Discard | Silence total — les données existent dans les artefacts. Perd la trace dans les logs harness ; un print inattendu disparaît sans indice. | |
| Buffer par session | Capturer dès maintenant dans un buffer que stop_session (ph17) renverra. Design prématuré — la forme du résumé structuré appartient à la ph17. | |

**User's choice:** stderr

### Q3 — Portée de SilenceUsage/SilenceErrors

| Option | Description | Selected |
|--------|-------------|----------|
| Commande mcp seule (Recommandé) | Cobra vérifie « commande exécutée OU root » — poser les flags sur newMCPCmd() suffit. UX CLI existante intacte. Erreurs mcp via zap→stderr. | ✓ |
| rootCmd global | Cohérent partout mais régression UX pour les utilisateurs CLI actuels (plus d'usage auto sur erreur de flag). | |

**User's choice:** Commande mcp seule

---

## Périmètre test pureté

### Q1 — Couverture du test de pureté ph16 (zéro tool en ph16)

| Option | Description | Selected |
|--------|-------------|----------|
| Harness + audit des seams (Recommandé) | Harness in-memory réutilisable (initialize, tools/list vide, méthode inconnue, erreur cobra) avec capture os.Stdout — étendu par ph17-19. PLUS test unitaire « seam audit » sur le constructeur de config MCP. Couvre « every seam » sans session réelle. | ✓ |
| Harness seul | Handshake + chemins d'erreur seulement ; le câblage pipeline vérifié par test seulement en ph17. | |
| Session simulée complète dès ph16 | Stubs de tools jetables — sur-ingénierie, travail jeté en ph17. | |

**User's choice:** Harness + audit des seams

### Q2 — Sémantique de l'assertion de pureté (in-memory)

| Option | Description | Selected |
|--------|-------------|----------|
| Zéro octet fuité (Recommandé) | En in-memory, les frames ne transitent pas par os.Stdout — tout octet capturé est une fuite : assertion len == 0. « Chaque ligne parse comme frame » = e2e stdio réel ph19 (SRV-04). | ✓ |
| Les deux dès ph16 | Second test sous-processus stdio réel dès ph16 — duplique le travail de ph19. | |

**User's choice:** Zéro octet fuité

---

## Claude's Discretion

- UX `cpg mcp` : flags hérités (--debug/--log-level/--json), buildLogger() inchangé, visibilité/description standard (zone « UX cpg mcp + logs » proposée mais non sélectionnée par l'utilisateur)
- Writer atomique (SEC-02) : miroir mécanique exact du motif evidence/health (same-dir temp, pas de fsync)
- Emplacement des fichiers de test/harness dans cmd/cpg

## Deferred Ideas

None — discussion stayed within phase scope.
