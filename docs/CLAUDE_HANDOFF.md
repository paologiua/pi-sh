# Passaggio di consegne a Claude Code

## Manutenzione del passaggio di consegne

Questo documento segue lo stato corrente del lavoro. Dopo ogni attività
completata e verificata, rimuovila dalla sequenza operativa e dal backlog,
aggiorna la prima priorità e correggi le descrizioni superate. Conserva qui
soltanto il contesto necessario alla ripresa; registra le evidenze dettagliate
nei documenti tecnici pertinenti. Un'implementazione con verifiche mancanti o
una causa ancora sconosciuta resta tra le attività aperte.

## Obiettivo e punto da cui ripartire

Continua il progetto esistente in `/Users/paologiua/Projects/pi-sh`. L'obiettivo
è la massima compatibilità comportamentale dimostrabile con OpenSSH, SCP e SFTP
tramite Raspberry Pi Connect ufficiale e l'agente già presente. Il progetto è
sperimentale e incompleto: le build riuscite non equivalgono al completamento.

Leggi prima `AGENTS.md`, `README.md`, `docs/SSH_COMPATIBILITY.md` e il contratto
completo: `/Users/paologiua/.codex/attachments/5e0f7c47-3546-42e9-8638-98c97c84485f/goal-objective.md`.
Questo documento indica il punto di ripresa; non riduce il contratto originale.
`docs/STATUS.md` contiene anche aggiornamenti storici: gli esiti recenti in testa
e `docs/REAL_DEVICE_TESTS.md` prevalgono sulle vecchie note di attesa.

**Prima priorità: voci aperte del backlog (vedi sotto), partendo dai difetti
emersi nella prova nativa CLI del 2026-10-22.** Su Pi sono verificati anche
pi-sftp/pi-cp di produzione (glob, tilde, ripresa, `-r -p`) e `pi-sh -v`, con
pulizia verificata (REAL_DEVICE_TESTS.md, NATIVE_CLI_PLAN.md). Lo stallo dell'input binario è stato diagnosticato, corretto
e verificato sul Pi il 2026-10-22, insieme al piano di trasferimento completo
(vedi sotto). Il lavoro complessivo rimane incompleto.

## Vincoli operativi

- Usa esclusivamente Connect come trasporto del prodotto. SSH è ammesso per
  diagnostica in lettura; i server OpenSSH locali sono riferimenti di test.
- Il Pi deve funzionare con software e configurazione esistenti. Nessuna
  installazione, servizio, riavvio, configurazione o script persistente remoto.
  Gli adattatori Bash del prodotto vengono inviati in memoria.
- Password/TOTP nei prompt locali del proprietario; sessioni in memoria.
  Non esportare cookie/token, credenziali, SDP/ICE o trascrizioni sensibili.
- Il proprietario ha risposto **«Autorizzo prove e pulizia»** al piano esatto
  `docs/NATIVE_TRANSFER_PLAN.md`. L'autorizzazione riguarda soltanto
  `/tmp/pi-sh-probe-7470c80bcf6721dd82f13486e142579c`, i figli elencati nel piano,
  le copie sintetiche, i permessi/timestamp e la rimozione verificata.
  Non estenderla ad altri percorsi, carichi o failure injection invasive.
- Non pubblicare, fare push o contattare Raspberry Pi. La licenza del progetto
  e le verifiche di condizioni del servizio prima della pubblicazione sono aperte.
- Le credenziali Connect e il permesso di controllo Chrome sono due passaggi
  distinti. Nessuno dei due va aggirato; chiedi intervento solo dopo il rifiuto
  effettivo. Non richiedere nuovamente l'autorizzazione alle stesse scritture.

## Ultime evidenze e stato lasciato

Lo stallo intermittente dell'input binario è risolto e verificato (2026-10-22).
Causa: Bash 5.2 (sul Pi 5.2.37) può segnalare il timeout di `read -t` dopo aver
consumato il newline; il broker teneva un record `I` completo come frammento e il
client attendeva `A`. Dettagli ed evidenze in `docs/SHELL_BROKER.md`.

| Prova (2026-10-22) | Esito |
| --- | --- |
| Locale: `TestShellBrokerMegabyteStdinCount/late-newlines`, broker vecchio | Bash 5.2.37: stallo 2/2; Bash 5.3: superato |
| Locale: stesso test dopo la correzione; suite race, vet, moduli, 7 target | Superati con entrambe le versioni |
| Pi: input1MiB senza SFTP su `wc -c` | 3/3: conteggio1048576 e CLI23 |
| Pi: piano `NATIVE_TRANSFER_PLAN.md` completo | Tutte le fasi superate una volta, compreso upload1MiB; pulizia con assenza verificata; broker code0 |

Sono singole esecuzioni, non prove di carico. Il passaggio da ~11KiB/s è un limite
di prestazioni (hex e stop-and-wait), non un errore. Resta non verificato il
cleanup dei discendenti del tentativo read-only fallito del 2026-10-21.
Prima di una prova reale serve un accesso recente: Remote shell in Connect,
poi pagina Devices, poi `fetch(…)` stampato dal comando nella console (digitando
prima `allow pasting`). Bash 5.2.37 di prova era in
`/private/tmp/claude-501/-Users-paologiua-Projects-pi-sh/9edee8ad-09c5-400f-b5b9-2aaba5b22f3f/scratchpad/bash52/bash-5.2/bash`
(effimero; ricostruibile da sorgenti GNU ufficiali e patch 001–037).

## Prossima sequenza di lavoro

1. Controlla stato Git e file attuali, poi leggi `docs/SSH_COMPATIBILITY.md` e i
   documenti dell'area scelta dal backlog.
2. Prosegui la milestone dei trasferimenti con le voci aperte del backlog File:
   prove native delle CLI `pi-cp`/`pi-sftp` (non solo del transfer-probe),
   glob/tilde, errori/interruzioni. Ogni
   nuova scrittura sul Pi richiede un'autorizzazione specifica: quella esistente
   copre solo il piano `NATIVE_TRANSFER_PLAN.md` e la sua root.
3. Registra separatamente risultati locali e sul Pi e aggiorna la matrice.

Comandi esistenti, da usare solo rispettando il piano e dopo aver ricostruito
lo stato corrente:

```sh
./bin/transfer-probe --execute-approved --browser \
  --root /tmp/pi-sh-probe-7470c80bcf6721dd82f13486e142579c raspberrypi

# Solo se una prova ha realmente creato quella root e non l'ha rimossa:
./bin/transfer-probe --execute-approved --cleanup-only --browser \
  --root /tmp/pi-sh-probe-7470c80bcf6721dd82f13486e142579c raspberrypi
```

I flag non costituiscono da soli autorizzazione. Il piano limita i contenuti
sintetici remoti a 1.314.816 byte, oltre all'overhead del filesystem.

## Cosa funziona già

- **Autenticazione/trasporto:** login HTTPS Go con password e TOTP, senza Chrome
  obbligatorio; companion browser opzionale; discovery, signaling e Pion WebRTC.
  Login diretto e comando reale con uscita 23 sono stati verificati sul Pi.
- **Comandi senza TTY:** broker Bash con stdin binario, stdout/stderr separati,
  EOF indipendente, stato finale, cancellazione e handshake di cleanup. Esistono
  prove native con tutti i valori dei byte, duplex e uscite/segnali selezionati.
- **Terminale:** shell interattiva con companion verificata; `-t/-tt/-T`,
  RequestTTY, TERM, modi canonici/raw e resize integrati nella CLI dei comandi.
  Sul Pi sono verificati dimensioni iniziali, resize, input, uscita 23 e handler
  di Ctrl-C/Ctrl-Z più Ctrl-D su riga canonica vuota. Non estendere questi esiti
  a sospensione reale, segnali predefiniti o cleanup dopo perdita del trasporto.
- **Trasferimenti:** get/put regolari, ripresa, `-p`, flush negoziato e ricorsione
  SCP hanno implementazioni e confronti locali. Sul Pi il piano completo (vuoto, 64KiB, 1MiB,
  preservazione, ripresa, flush, piccolo albero ricorsivo, errore, pulizia) è
  passato una volta tramite transfer-probe, 2026-10-22.
- **CLI/configurazione:** subset esplicito di `-F/-o`, Host/Include/HostName,
  precedenza, `-G/-V/-Q`, escape e opzioni dopo la destinazione fino al comando.
  Ci sono confronti locali con OpenSSH_10.2p1 / LibreSSL 3.3.6.
- **Integrato il 2026-10-22 (solo locale, Pi da verificare):** diagnostica
  `-v/-q/-E/-y`, LogLevel, SyslogFacility con redazione strutturale
  (`internal/diag`, DIAGNOSTICS.md); `Match`, `Tag`, `-P` (CONFIGURATION.md);
  destinazioni `user@host`/URI/IPv6 e operandi scp/sftp (`internal/sshdest`),
  rifiuto di utente/porta/IP, BatchMode, ConnectTimeout/ConnectionAttempts,
  ServerAlive via ICE (`internal/connpolicy`, CONNECTION_POLICY.md); glob e
  tilde in pi-sftp/pi-cp (`internal/pathglob`, GLOB_TILDE.md). Decisioni del
  proprietario in CONNECTION_POLICY.md.

## Correzioni recenti da preservare

### Difetti aperti dalla prova CLI nativa (2026-10-22)

- La redazione della diagnostica è eccessiva: `version=[redacted]` e
  `session:[redacted]` nei messaggi `-v` (internal/diag).
- Il run di `pi-sh` fallito per stdin locale (prima della correzione di
  `localio` per /dev/null su macOS) ha riportato cleanup del broker non
  confermato: capire perché l'abort dopo un errore di stdin non riceve `D`.
- Proposta del proprietario da progettare: `-l email` o `email@device` per
  scegliere l'account Raspberry Pi ID nel login diretto (CONNECTION_POLICY.md).
- Per le prove con il companion, Claude in Chrome può eseguire il `fetch`
  nella pagina Devices; non attendere la Promise (resta aperta per tutta la
  sessione) e leggere l'URL dal log subito, senza redirezioni nascoste.

### Test TTY da osservare

`TestStagedTTYStopOpenSSHDifferential/caught-suspend` è fallito una volta in una
suite completa (2026-10-22) e poi è passato più volte. Tocca il TTY adapter dove
il `cat` inattivo ignora i segnali di job control: se si ripete, indagare lì.

### Letture temporizzate nel broker (Bash 5.2)

In `broker.bash` e `tty.bash` nessun `read -t` con timeout non nullo deve leggere
dati di protocollo: Bash 5.2 può restituire timeout dopo aver consumato il
delimitatore. Sonda con `read -t 0`, leggi senza timeout, attendi sulla pipe
inattiva. Non rimuovere il relay `cat` dello stdin del broker (buffer TTY di Bash
su macOS) né i segnali ignorati dal `cat` inattivo del TTY adapter. Esegui
`TestShellBrokerMegabyteStdinCount` anche con Bash 5.2.x. Vedi SHELL_BROKER.md.

### Input TTY e chiusura anticipata

`cmd/pi-sh/input.go` raggruppa soltanto UTF-8 già disponibile in messaggi fino
a 4 KiB, svuotandoli prima di letture bloccanti e azioni locali. Prima, una
paste da 64 KiB generava 65.536 messaggi e poteva riempire la coda da 4.096 frame
prima dell'invio dello stato finale. Non aumentare code o accettare EOF senza
stato per nascondere il problema. Restano il drain limitato dopo errore input
e la validazione rigorosa di TR/TX. Vedi `docs/TTY_COMPLETION.md`.

### Metadati di upload tramite handle

`internal/sftpcli/preserve.go` ora invia modo e atime/mtime con un singolo
FSETSTAT sull'handle aperto, tramite `File.SetPermissionsAndTimes`.
Il test locale rinomina il file aperto e crea un sostituto al vecchio percorso:
solo l'inode originario deve ricevere i metadati. Il vecchio codice falliva quel
test; il nuovo passa, insieme ai confronti OpenSSH e ai guasti READ/WRITE/CLOSE.
Gli errori di stato dei metadati restano advisory; non possono cancellare errori
di copia, protocollo o CLOSE. Restano race sui percorsi di directory/download,
attributi omessi indistinguibili da valori zero e assenza di ACL/ownership/bit speciali.
Vedi `docs/SFTP_METADATA.md`.

### Fork locale di pkg/sftp

`go.mod` usa `replace github.com/pkg/sftp => ./third_party/sftp`, base v1.13.11.
Tutti i 65 file upstream nel manifest sono invariati. Le aggiunte sono
`client_stream.go`, `client_metadata.go` e i rispettivi due test. ReadDirEach
consuma pacchetti NAME limitati, conserva errori di CLOSE e valida nomi/risposte.
È usato dai download SCP ricorsivi e da `ls -f`. `ls` ordinato accumula ancora
l'intera directory. Il test di provenienza rifiuta modifiche non registrate;
`go mod verify` da solo non verifica un replace locale. Leggi
`third_party/sftp/PI_SH_CHANGES.md` prima di modificare il fork.

## Backlog necessario per il goal completo

| Area | Lavoro ancora necessario | Documenti da leggere quando la affronti |
| --- | --- | --- |
| Terminale/lifecycle | Shell interattiva diretta senza companion; sessioni lunghe; segnali predefiniti; sospensione/ripresa reale; job annidati; perdita rete/autorizzazione; verifica discendenti e cleanup; escape mancanti | TTY_COMMANDS, TTY_JOB_CONTROL, TTY_COMPLETION, COMMAND_CANCELLATION, ESCAPES |
| File | Prove native delle CLI pi-cp/pi-sftp oltre al transfer-probe; verifica sul Pi di glob/tilde; `~user` (richiede expand-path nel fork); prove ripetute/più grandi; ricorsione console SFTP; inventario nomi/piattaforme; hard link/sparse/>32 bit; disk-full/permessi/interruzioni; progress/banda; listing ordinato con memoria limitata; comandi SFTP mancanti | SFTP_BACKEND, SFTP_METADATA, SFTP_RESUME, SFTP_FLUSH, SCP_RECURSION, SFTP_LISTING |
| CLI/config | Verifica sul Pi di Match e policy di connessione (diagnostica `-v` verificata); proposta `-l email` per l'account Connect; CanonicalizeHostname; configurazioni predefinite; resto dei token/direttive; famiglia IP/binding; ambiente/RemoteCommand/LocalCommand/background/no-shell | CLI_OPTIONS, CONFIGURATION, LOCAL_INSPECTION, SSH_COMPATIBILITY |
| Forwarding | Capacità reali dell'agente; eventuali utility già presenti; -W/-L/-R/-D, half-close, concorrenza, bind/IPv6, cleanup, multiplexing e operazioni di controllo | FORWARDING, PROTOCOL, SERVICE_CONSTRAINTS |
| Portabilità | Esecuzione reale Linux/Windows e iSH fisico; memoria/syscall/rete/terminale; iSH non è verificato dalla sola compilazione 386 | ISH_COMPATIBILITY, LOCAL_IO, TERMINAL, TESTING |
| Sicurezza/rilascio | Sessioni lunghe/scadenza/revoca; dipendenze e fork; licenza originale da scegliere; condizioni del servizio prima di pubblicare | SECURITY, DEPENDENCIES, THIRD_PARTY_NOTICES |

Forwarding oggi non è implementato. Il candidato Bash `/dev/tcp` è stato
dimostrato insufficiente per half-close con lettore vivo; questo esclude quel
candidato, non ogni soluzione. `nc` funziona nel fixture sul Mac ma dialetto,
presenza e comportamento sul Pi sono ignoti. Non installarlo e non inventare
canali Connect. Opzioni SSH senza equivalente devono essere rifiutate chiaramente.

La discrepanza isolata sugli atime nelle prove ricorsive è stata classificata
(2026-10-22) come lettore esterno sul Mac di test (probabilmente Trend Micro):
il test ora riporta un file di controllo intatto in caso di fallimento.
Vedi `docs/SFTP_LISTING.md`.

## Toolchain, test e stato della working tree

Il progetto ha il commit iniziale `58181d3` su `main` (2026-10-22, richiesto dal
proprietario). Nessun push. Non usare reset/clean o eliminare file come fossero
scarti. Ricontrolla con `git status`.

Ambiente locale usato nell'ultima verifica:

```sh
export PATH=/private/tmp/pi-sh-toolchain/go/bin:$PATH
export GOCACHE=/private/tmp/pi-sh-gocache
export GOMODCACHE=/private/tmp/pi-sh-gomodcache
export GOTOOLCHAIN=go1.27.3
export PI_SH_TEST_BASH=/private/tmp/bash-5.3/bash

go test -race ./... -timeout=150s
go test -C third_party/sftp -race ./... -timeout=60s
go vet ./...
go vet -C third_party/sftp -stdmethods=false ./...
go mod verify
```

I percorsi in `/private/tmp` sono locali ed effimeri: verificane l'esistenza.
Il binario Go nel PATH risolve il toolchain 1.27.3 dalla cache verificata; senza
GOMODCACHE il tentativo di download può finire nella directory utente protetta.
La suite completa richiede PTY, ispezione processi e listener loopback: il sandbox
li bloccava, mentre l'esecuzione locale normalmente autorizzata è passata.
Non correggere il prodotto per mascherare un rifiuto del sandbox.

Ultimo stato delle verifiche **locali**: (2026-10-22) suite race completa con Bash 5.3,
pacchetti shellbroker e cmd con Bash 5.2.37, vet, verifica moduli e i sette
target CGO-disabled passati dopo la correzione; prima: suite race completa passata; race del
modulo annidato passata; root vet e verifica moduli passati. Il vet annidato
completo segnala due firme ReadFrom upstream invariate: solo per quel modulo
è stato escluso l'analizzatore stdmethods; il root vet resta completo.

Build e compilazione dei test passate, con CGO disabilitato, su darwin/amd64,
darwin/arm64, linux/amd64, linux/arm64, linux/386 con GO386=softfloat,
windows/amd64 e windows/arm64. Compilati anche i test del fork per ogni target.
I binari Mac `pi-sh`, `pi-cp`, `pi-sftp`, `transfer-probe` sono stati ricostruiti.
Il workflow `.github/workflows/check.yml` include i controlli; non è stata
eseguita una pipeline remota GitHub durante questa sessione.

La toolchain minima è stata aggiornata a Go 1.27.3 per una vulnerabilità x509;
x/crypto è v0.57.0, x/net v0.60.0. L'ultimo govulncheck sorgente con v1.9.0 ha
segnalato zero vulnerabilità raggiungibili, zero nei pacchetti importati e una
nel modulo crypto/openpgp non importato, senza correzione disponibile. Non
descrivere il risultato come assenza di ogni vulnerabilità o audit definitivo.

## Criterio di conclusione

Per ogni voce della matrice servono comportamento previsto, implementazione,
test automatici, evidenza reale al livello dichiarato, differenze e blocchi.
Una voce ignota, parziale o con prove mancanti resta aperta. Un'esclusione richiede
un vincolo o blocco dimostrato e le alternative consentite esaminate. Dopo ogni
milestone aggiorna matrice e documenti pertinenti. Non dichiarare il goal
completato per un probe riuscito, una build verde o una singola funzionalità.
