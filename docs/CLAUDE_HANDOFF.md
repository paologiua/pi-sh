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
  Il 2026-10-26 sono state lette le condizioni di Connect (versione 20 ottobre
  2026, https://connect.raspberrypi.com/terms). Clausole rilevanti: §2.8(a)(ii)
  divieto di reverse engineering, salvo le eccezioni di legge per
  l'interoperabilità; §2.8(e) divieto di aiutare terzi ad accedere in modo
  diverso da quanto previsto; §11.5 riservatezza, che include i dettagli del
  servizio e i risultati dei test di prestazioni; §10.3 marchi; §14.3 chiusura
  dell'account senza preavviso. Indicazioni date al proprietario (non è
  consulenza legale): chiedere il permesso a Raspberry Pi (lo fa il proprietario),
  oppure pubblicare senza i documenti di ricerca sul protocollo e senza le
  misure di prestazioni; aggiungere una licenza; mantenere l'avviso «non
  ufficiale»; non usare loghi. Il proprietario non ha ancora deciso.
  Il 2026-10-26 la pagina Devices annunciava un aggiornamento dello Schedule 2
  delle condizioni, in vigore dal 27 ottobre 2026: non ancora letto.
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

Sono singole esecuzioni, non prove di carico. Dal 2026-10-26 il broker usa un
percorso dati in pipeline (ramo integrato): sul Pi upload ≈186 KiB/s, stdout e
stderr ≈18 KiB/s (prima ~11 KiB/s); l'output resta limitato dalla lettura byte per
byte in Bash (SHELL_BROKER.md). Nella stessa prova l'abort dopo errore di stdin
locale ha confermato il cleanup (una esecuzione). Resta non verificato il
cleanup dei discendenti del tentativo read-only fallito del 2026-10-21.
Prima di una prova reale serve un accesso recente: Remote shell in Connect
(il pulsante Connect apre un popup fuori dal gruppo di tab di Claude in Chrome;
basta aprire nella tab `/devices/<id>/remote-shell-session`, attendere il
prompt senza digitare nulla), poi pagina Devices, poi `fetch(…)` stampato dal comando nella console (digitando
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

### Rami integrati (2026-10-26/27)

Tutti i rami degli agenti sono integrati in `main` e verificati sul Pi
(REAL_DEVICE_TESTS.md): broker (throughput, cleanup dopo errore di stdin),
terminale (shell di login tramite adattatori, ora percorso interattivo
predefinito; `--legacy-shell` per il flusso grezzo; tutti gli escape; `~^Z` e
SIGTSTP esterno; `~.` esce 255) e forwarding (`-L`, `-D`, `-W`, `-N` su una
sessione Connect dedicata; FORWARDING.md). Il companion del browser ora serve
più offerte con un solo heartbeat: prima la seconda sessione dello stesso
processo (forwarding accanto a un comando, oppure i tentativi di
ConnectionAttempts in modalità browser) andava in timeout.

Aperti: M3 di `scripts/native-shell-check.sh` (perdita di rete: il proprietario
deve disattivare la rete del Mac); la riga
fatale `Error: remote port forwarding failed…` esce con il prefisso `pi-sh:`,
che ssh non mette (convenzione generale del logger, DIAGNOSTICS.md: valutare
per tutte le righe fatali). Half-close con -L, `-D` e `-R` (diretto, SOCKS
inverso, porta privilegiata rifiutata) sono verificati sul Pi
(REAL_DEVICE_TESTS.md, 2026-10-27/28).

Velocità del forwarding (2026-10-28, FORWARDING.md "Throughput"): adattatore con
finestre 8×2 KiB, `read -N`, lettore per flusso, e Perl che codifica da solo i
record hex; Perl ora è preferito a nc. Sul Pi: Perl ~600 KiB/s Pi→Mac e
~450 KiB/s Mac→Pi (prima 12 e 25), nc 17 e 243. Chiusura dopo EOF ora svuota
l'input in coda (`G`) e l'ascoltatore -R scrive i dati bufferizzati prima di
chiudere (difetto emerso con la velocità). Rifatti sul Pi con piano approvato (NATIVE_FORWARD_PLAN.md): half-close 1 MiB
in 2,2 s con Perl e 4,0 s con nc, -R 1 MiB in ~1,8 s con `~#`, `-R 0` allocato.

Il ramo CLI (account `-l`/`email::device`/`User`/predefinito, decisione su ogni
opzione, redazione corretta) è integrato e verificato (28 pacchetti, race). Decisioni
aperte del proprietario: SetEnv/SendEnv (prefisso di assegnazioni validate?),
AddressFamily/BindAddress (equivalente fattibile, non fatto). Prove native in sola
lettura del ramo CLI richiedono login diretto (password/TOTP del proprietario).

### Difetti aperti dalla prova CLI nativa (2026-10-22)

- Per le prove con il companion, Claude in Chrome può eseguire il `fetch`
  nella pagina Devices; non attendere la Promise (resta aperta per tutta la
  sessione) e leggere l'URL dal log subito, senza redirezioni nascoste.

### Fixture `caught-suspend` (risolto 2026-10-26)

L'intermittenza era della Bash 3.2 di macOS (`/bin/sh`) usata come shell del
comando: perde la riga se un trap TSTP interviene durante `read` (3/100 su PTY
nudo, senza pi-sh). I fixture usano la Bash moderna, ripetono `read` interrotto
e inviano la riga dopo il trap (TESTING.md). Non attribuirlo all'adattatore.

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

## Revisione del codice e prove da approvare (2026-10-28)

Revisione completa in quattro aree: difetti corretti con test e quelli registrati
ma non modificati in `docs/CODE_REVIEW_2026-10-28.md`. Il piano delle prove
sul Pi che aprono porte (`docs/NATIVE_FORWARD_PLAN.md`) è stato approvato ed
eseguito: ha fatto emergere una gara alla chiusura del data channel (l'agente
chiude con ABORT SCTP «User Initiated Abort», Pion chiama OnError e OnClose in
goroutine diverse; un comando finito usciva a volte con 255), corretta con test.

## Revisione della matrice (2026-10-28)

Ogni riga di `docs/SSH_COMPATIBILITY.md` è stata ricontrollata: tutti i test citati
esistono (controllo automatico dei nomi), le prove native corrispondono a
REAL_DEVICE_TESTS.md, le opzioni che risultavano `U` ma sono rifiutate con motivo
(scp `-O`, `-3`, `-R`, `-D`, `-S`, `-X`; sftp `-R`, `-r`) sono indicate come tali,
`-Z` (inesistente in OpenSSH 10.2) e fingerprint/visual host key sono esclusi con
motivo e test, «Not applicable» indica comportamenti decisi solo sul client.
Righe ancora aperte (lavoro rimanente):

- Policy di connessione: ConnectTimeout verificato sul Pi in modalità browser
  (2026-10-28); ConnectionAttempts (solo login diretto) senza prova nativa; ServerAlive/TCPKeepAlive e perdita di rete (M3, serve il
  proprietario); scadenza/revoca/riconnessione e sessioni lunghe.
- Sessioni multiple: cinque insieme verificate (2026-10-28); oltre e quote non provate; stress di localio con il Pi.
- SCP: nomi con newline/due punti/trattino iniziale e disk-full sul Pi (servono
  scritture, quindi un piano); più sorgenti e download ricorsivo verificati
  2026-10-28; `-X`, `-3`/`-R`, `-O` aperti (rifiutati).
- SFTP: `mget`/`mput`, `-r` della console, flag di `ls`, `lumask`, `ln`, `chown`/
  `chgrp`, `df`/`version`, `lls`/`lmkdir`/`!`, `progress`, `-R`; file sparsi e >32 bit.
- Forwarding: bind jolly/IPv6 di -R verificati sul Pi (2026-10-28, NATIVE_RBIND_PLAN.md); -R con molte connessioni verificato (16/64/65, tutte complete, ~80 KiB/s per
  direzione: da migliorare unendo i frame piccoli in record);
  EnableEscapeCommandline. Molti stream (64) e una sessione di 23 minuti
  verificati 2026-10-28, dopo due correzioni (connettore Perl che svuota il
  socket, adattatore che elabora più record per giro: FORWARDING.md "Many
  streams").
- Diagnostica: prefisso `pi-sh:` sulle righe fatali; manca `Connection to HOST
  closed.` di ssh con -tt.
- Decisioni del proprietario: SetEnv/SendEnv, AddressFamily/BindAddress;
  LocalCommand e askpass aperti; `~&` escluso (Go non può fare fork).
- iSH runtime, termini di Connect, licenza e pubblicazione: in mano al proprietario.

## Backlog necessario per il goal completo

| Area | Lavoro ancora necessario | Documenti da leggere quando la affronti |
| --- | --- | --- |
| Terminale/lifecycle | Shell interattiva diretta senza companion; sessioni lunghe; segnali predefiniti; sospensione/ripresa reale; job annidati; perdita rete/autorizzazione; verifica discendenti e cleanup; escape mancanti | TTY_COMMANDS, TTY_JOB_CONTROL, TTY_COMPLETION, COMMAND_CANCELLATION, ESCAPES |
| File | Prove native delle CLI pi-cp/pi-sftp oltre al transfer-probe; verifica sul Pi di glob/tilde; `~user` (richiede expand-path nel fork); prove ripetute/più grandi; ricorsione console SFTP; inventario nomi/piattaforme; hard link/sparse/>32 bit; disk-full/permessi/interruzioni; progress/banda; listing ordinato con memoria limitata; comandi SFTP mancanti | SFTP_BACKEND, SFTP_METADATA, SFTP_RESUME, SFTP_FLUSH, SCP_RECURSION, SFTP_LISTING |
| CLI/config | Verifica sul Pi di Match e policy di connessione (diagnostica `-v` verificata); proposta `-l email` per l'account Connect; CanonicalizeHostname; configurazioni predefinite; resto dei token/direttive; famiglia IP/binding; ambiente/RemoteCommand/LocalCommand/background/no-shell | CLI_OPTIONS, CONFIGURATION, LOCAL_INSPECTION, SSH_COMPATIBILITY |
| Forwarding | -R implementato e verificato sul Pi (2026-10-28, regole sshd `GatewayPorts clientspecified`; dopo il cambio dei record rifatti sul Pi half-close, velocità, `~#` e `-R 0`); molti stream, sessioni lunghe, perdita di connessione | FORWARDING, PROTOCOL, SERVICE_CONSTRAINTS |
| Portabilità | Esecuzione reale Linux/Windows e iSH fisico; memoria/syscall/rete/terminale; iSH non è verificato dalla sola compilazione 386 | ISH_COMPATIBILITY, LOCAL_IO, TERMINAL, TESTING |
| Sicurezza/rilascio | Sessioni lunghe/scadenza/revoca; dipendenze e fork; licenza originale da scegliere; condizioni del servizio prima di pubblicare | SECURITY, DEPENDENCIES, THIRD_PARTY_NOTICES |

Il forwarding usa l'adattatore Bash in memoria e Perl tramite `perl-base`
(Essential, verificato sul Pi il 2026-10-27; FORWARDING.md "Perl connector"),
altrimenti il `nc.openbsd` già presente (OpenBSD netcat 1.229, `-N`). Python non si
usa (priorità optional). Il candidato Bash `/dev/tcp` resta escluso per
l'half-close. Non installare connettori e non inventare canali Connect. Opzioni
SSH senza equivalente devono essere rifiutate chiaramente.

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
export PATH=$HOME/Library/Caches/pi-sh-go/go-official/bin:$PATH
export GOCACHE=$HOME/Library/Caches/pi-sh-go/build
export GOMODCACHE=$HOME/Library/Caches/pi-sh-go/mod
export GOTOOLCHAIN=local
export GOPROXY=https://proxy.golang.org,direct
export PI_SH_TEST_BASH=/private/tmp/bash-5.3/bash

go test -race ./... -timeout=150s
go test -C third_party/sftp -race ./... -timeout=60s
go vet ./...
go vet -C third_party/sftp -stdmethods=false ./...
go mod verify
```

Dal 2026-10-26 la toolchain è Go 1.27.3 ufficiale (SHA-256 verificato da go.dev)
in `~/Library/Caches/pi-sh-go`: macOS aveva ripulito `/private/tmp`, danneggiando
la vecchia toolchain e la cache dei moduli. Bash 5.3 di test resta in `/private/tmp`
(ricostruibile) e può sparire allo stesso modo: verificane l'esistenza.
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
