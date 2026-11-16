# Otevřené body

## Stav (2026-11-16)
- Stav: Stage 1 má připravenou konfiguraci Buildroot/uClibc-ng, minimálního chrootu a ruční instalaci/start/stop SSH na 2222 pouze s klíčem; 14 lokálních testů prošlo. Úplný build a rootfs/ELF audit nebyly provedeny kvůli blokovaným downloadům; kompatibilita s kernelem 2.6.31.8 ani SSH na NASu není potvrzená.
- Další krok: Na Ubuntu s dostupnými veřejnými zdroji provést připnutý build a audit, vyhodnotit případné chyby starých hlaviček a teprve po samostatném souhlasu provést shell probe a SSH/stop test; bez bootování a bez Stage 2.

## Čeká na rozhodnutí
- [ ] Před budoucím SSH testem dodat jeden administrátorský veřejný Ed25519 klíč a schválit konkrétní samostatný adresář a existující LAN IP; přístupové heslo ze secret.env není pro přípravu ani build potřeba. (2026-11-16)
- [ ] Potvrdit důležitost dat na existujícím ext4 svazku /dev/md0 před případnými budoucími pokusy; existence souborového systému nepotvrzuje zdraví disku ani bezpečnost původní inicializace. (2026-11-16)
- [ ] Pro případné budoucí bootovací experimenty mimo Stage 1 zajistit vhodný USB–TTL adaptér a ověřit pinout a napětí konkrétní desky; připojení a jakýkoli restart musí být samostatně schválené. (2026-11-16)

## Nedodělané
- [ ] Dokončit skutečný build Stage 1 z připnutých zdrojů a archivovat vyřešenou konfiguraci a audit ELF/ABI, knihoven, symbolů a velikosti rootfs; místní curl nemá funkční DNS, hotové ARM binárky zatím nevznikly. (2026-11-16)
- [ ] Ověřit kombinaci uClibc-ng 1.0.59, hlaviček 2.6.31.8 a aktuálních balíčků skutečným překladem; při chybě neřešit build tichým zvýšením verze hlaviček ani opuštěnou distribucí. (2026-11-16)
- [ ] Po samostatném souhlasu ověřit Stage 1 shell, vendor EABI a syscall chování, soukromé devpts, entropii, klíčové SSH přihlášení na 2222 a úplný stop; lokální testy používají simulované mounty, chroot a procesní pohledy a nic na NASu nespouštěly. (2026-11-16)
- [ ] Doověřit PCB revizi, konkrétní SoC a jeho takt, síťový čip a řadič ventilátoru; A203 a CPU jádro Feroceon samy neurčují správný profil. (2026-11-16)
- [ ] Vysvětlit rozpor kernel_addr=480000 v exportu prostředí proti začátku kernel_1 na 0xC80000 podle uloženého mtdparts; žádnou hodnotu bez ověření nepoužívat k bootování ani zápisu. (2026-11-16)
- [ ] Zjistit skutečné USB/FAT32 chování bootloaderu, podporu kernelu a DT/ATAG handoff a návrat při selhání; statické záznamy příkazů nejsou funkční test. (2026-11-16)
- [ ] Pokud se bude zvažovat kexec, získat důkaz CONFIG_KEXEC a konkrétní implementace běžícího kernelu; verze kernelu ani root shell jeho použitelnost nepotvrzují. (2026-11-16)
- [ ] Doplnit původní nástroj a volby zálohování, ECC/OOB a bad-block politiku a kvalifikovat obnovu; devět ověřených hashů a oddělení OOB nepotvrzují obnovitelnost a analýza ECC neopravovala. (2026-11-16)
- [ ] Dokončit offline posouzení původní dokumentace výrobce a komunitních návodů; přímý archiv výrobce a Doozan nebyly dostupné, historické návody nejsou schválený postup. (2026-11-16)
- [ ] Před přijetím ADR ověřit bezpečnostní životní cyklus vybrané větve OpenWrt, potřebné balíčky a rozdíly NSA310 proti profilu NSA310b. (2026-11-16)
- [ ] Změřit rozpočet RAM, velikosti systému a výkonu TLS/SMB/SFTP i možnost bezpečného chlazení; uvedené limity jsou cíle, nikoliv výsledky. (2026-11-16)
- [ ] Upřesnit podepisování aktualizací, důvěryhodné klíče, migrace konfigurace a recovery při výpadku napájení bez předpokladu dvou NAND slotů; mechanismus dosud není implementovaný. (2026-11-16)
- [ ] Začlenit tools/hamaproject.patch do přehledu a ověřit skutečně vydaný portfolio.json po standardním nasazení; dřívější typecheck a místní sběr prošly, 10 HTTP testů blokoval listen EPERM a HamaProject je mimo povolený zápis relace, body jsou zatím pouze lokální; podklad Stage 1 je aktualizován lokálně, skutečné nasazení a ověření webu zůstává nedokončené. (2026-11-16)

## Dokončené etapy
- Založen dokumentační bootstrap, ADR, návrh HAL, bezpečnostní model a plán obnovy bez zásahu do zařízení. (2026-11-16)
- Dokončena lokální analýza OOB struktury a U-Bootu, zapsán root Telnet a ověřené hardwarové údaje a posouzeny možnosti bootu bez UART; originální mtd0 nezměněn. (2026-11-16)
- Připraven Stage 1 build postup a ruční chroot/SSH controller; 14 bezpečnostních a ABI kontrol prošlo lokálně, úplný build a NAS testy zůstávají neověřené. (2026-11-16)
