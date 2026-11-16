# Otevřené body

## Stav (2026-11-16)
- Stav: Stage 1 (chroot na původním kernelu) zastaven po neúspěšném buildu uClibc-ng proti hlavičkám 2.6.31.8; projekt přechází na kvalifikaci OpenWrt. Na NASu se nic neměnilo, zálohy NAND jsou mimo repozitář.
- Další krok: připravit seznam čistě čtecích diagnostických příkazů pro stávající root Telnet a porovnat výsledky s profilem OpenWrt NSA310b; souběžně pořídit USB–TTL adaptér.

## Čeká na rozhodnutí
- [ ] Schválit konkrétní seznam čtecích příkazů přes root Telnet (SoC, síť, ventilátor, CONFIG_KEXEC, MTD/U-Boot prostředí), až bude připravený; bez toho nelze ověřit, že profil OpenWrt NSA310b sedí na A203. (2026-11-16)
- [ ] Před dočasným bootem OpenWrt (M3) zajistit vhodný USB–TTL adaptér (3,3 V) a ověřit pinout a napětí konkrétní desky; připojení a jakýkoli restart musí být samostatně schválené. (2026-11-16)

## Nedodělané
- [ ] Po příštím vypnutí/zapnutí NASu ověřit, že Telnet naběhne sám: boot hook `telnetd_boot` je na disku md0 (zy-pkgs init.d + USRPKG_DEPS_START), spouští ho zypkg_controller.sh ze rcS2; bez disku md0 nenaběhne. Telnet je nešifrovaný, cílově nahradit SSH s klíčem v OpenWrt. (2026-11-16)
- [ ] Ověřit zdraví disku pod /dev/md0 (SMART, vadné sektory); vlastník potvrdil, že svazek je prázdný po formátu a lze ho volně použít, zdraví disku tím ale ověřené není. (2026-11-16)
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
- Stage 1 vyhodnocen a zastaven: build uClibc-ng 1.0.59 narazil na glibc podmínku v linux/socket.h z 2.6.31.8; rozbité buildy a cache smazány, další směr OpenWrt. (2026-11-16)
- Po opakovaných pádech VM (příčina na straně Proxmoxu vyřešena) obnoven vynulovaný checkout Buildrootu a poškozený git repozitáře, smazány rozbité buildy a cache. (2026-11-16)
- Opraven Stage 1 build, který zamrazil VM (linux.git klonovaný do tmpfs /tmp): hlavičky z kernel.org tarballu s hashem, zákaz tmpfs cest a -j podle volné RAM. (2026-11-16)
- Založen dokumentační bootstrap, ADR, návrh HAL, bezpečnostní model a plán obnovy bez zásahu do zařízení. (2026-11-16)
- Dokončena lokální analýza OOB struktury a U-Bootu, zapsán root Telnet a ověřené hardwarové údaje a posouzeny možnosti bootu bez UART; originální mtd0 nezměněn. (2026-11-16)
- Připraven Stage 1 build postup a ruční chroot/SSH controller; 14 bezpečnostních a ABI kontrol prošlo lokálně, úplný build a NAS testy zůstávají neověřené. (2026-11-16)
