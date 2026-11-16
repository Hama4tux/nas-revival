# Otevřené body

## Stav (2026-11-16)
- Stav: Vlastník potvrdil root Telnet, hardware NSA310 A203 a devět OOB záloh mimo Git; lokální analýza potvrdila strukturu mtd0 a U-Boot 1.1.4 / Marvell 3.4.19. USB příkazy jsou kompilované, ale bezpečný jednorázový boot bez UART ani kexec není ověřený; do NASu nebylo zasahováno.
- Další krok: Zachovat NAND boot, vyjasnit nesoulad uložené adresy kernelu s MTD mapou a připravit kvalifikovaný UART přístup pro případnou samostatně schválenou fázi; žádné automatické pokračování do boot testu.

## Čeká na rozhodnutí
- [ ] Potvrdit důležitost dat na existujícím ext4 svazku /dev/md0 před případnými budoucími pokusy; existence souborového systému nepotvrzuje zdraví disku ani bezpečnost původní inicializace. (2026-11-16)
- [ ] Zajistit vhodný USB–TTL adaptér a ověřit pinout a napětí konkrétní desky; připojení a jakýkoli restart musí být samostatně schválené. (2026-11-16)

## Nedodělané
- [ ] Doověřit PCB revizi, konkrétní SoC a jeho takt, síťový čip a řadič ventilátoru; A203 a CPU jádro Feroceon samy neurčují správný profil. (2026-11-16)
- [ ] Vysvětlit rozpor kernel_addr=480000 v exportu prostředí proti začátku kernel_1 na 0xC80000 podle uloženého mtdparts; žádnou hodnotu bez ověření nepoužívat k bootování ani zápisu. (2026-11-16)
- [ ] Zjistit skutečné USB/FAT32 chování bootloaderu, podporu kernelu a DT/ATAG handoff a návrat při selhání; statické záznamy příkazů nejsou funkční test. (2026-11-16)
- [ ] Pokud se bude zvažovat kexec, získat důkaz CONFIG_KEXEC a konkrétní implementace běžícího kernelu; verze kernelu ani root shell jeho použitelnost nepotvrzují. (2026-11-16)
- [ ] Doplnit původní nástroj a volby zálohování, ECC/OOB a bad-block politiku a kvalifikovat obnovu; devět ověřených hashů a oddělení OOB nepotvrzují obnovitelnost a analýza ECC neopravovala. (2026-11-16)
- [ ] Dokončit offline posouzení původní dokumentace výrobce a komunitních návodů; přímý archiv výrobce a Doozan nebyly dostupné, historické návody nejsou schválený postup. (2026-11-16)
- [ ] Před přijetím ADR ověřit bezpečnostní životní cyklus vybrané větve OpenWrt, potřebné balíčky a rozdíly NSA310 proti profilu NSA310b. (2026-11-16)
- [ ] Změřit rozpočet RAM, velikosti systému a výkonu TLS/SMB/SFTP i možnost bezpečného chlazení; uvedené limity jsou cíle, nikoliv výsledky. (2026-11-16)
- [ ] Upřesnit podepisování aktualizací, důvěryhodné klíče, migrace konfigurace a recovery při výpadku napájení bez předpokladu dvou NAND slotů; mechanismus dosud není implementovaný. (2026-11-16)
- [ ] Začlenit tools/hamaproject.patch do přehledu a ověřit skutečně vydaný portfolio.json po standardním nasazení; dřívější typecheck a místní sběr prošly, 10 HTTP testů blokoval listen EPERM a HamaProject je mimo povolený zápis relace, body jsou zatím pouze lokální; aktuální úkol aktualizaci přehledu nezahrnuje. (2026-11-16)

## Dokončené etapy
- Založen dokumentační bootstrap, ADR, návrh HAL, bezpečnostní model a plán obnovy bez zásahu do zařízení. (2026-11-16)
- Dokončena lokální analýza OOB struktury a U-Bootu, zapsán root Telnet a ověřené hardwarové údaje a posouzeny možnosti bootu bez UART; originální mtd0 nezměněn. (2026-11-16)
