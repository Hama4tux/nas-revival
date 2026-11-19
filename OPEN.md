# Otevřené body

## Stav (2026-11-19)
- Stav: Čtecí diagnostika na NASu potvrdila, že hardware (procesor 88F6281 A1, ventilátor LM85, síť RTL8111D) odpovídá profilu OpenWrt NSA310b. Hotový je i ověřený build a simulace v QEMU. Na NASu se nic neměnilo.
- Další krok: pořídit USB–TTL adaptér (3,3 V) a dočasně spustit standardní initramfs OpenWrt NSA310b z RAM (M3). Bez UART to nejde, protože původní jádro nemá kexec.

## Čeká na rozhodnutí
- [ ] Vlastník objednal USB–TTL adaptér CP2102. Až přijde, otevřít kryt a zjistit, jestli je konektor konzole osazený kolíky. Pájet vlastník nebude: když kolíky chybí, zkusit kontakt bez pájení (kolíková lišta zasunutá šikmo do otvorů); jinak rozhodnout o přímém flashi NAND ze stock Linuxu, který nemá žádnou cestu záchrany. (2026-11-19)
- [ ] Před dočasným bootem OpenWrt (M3) zajistit vhodný USB–TTL adaptér (3,3 V) a ověřit pinout a napětí konkrétní desky; připojení a jakýkoli restart musí být samostatně schválené. (2026-11-16)

## Nedodělané
- [ ] NAS se po výpadku napájení nezapne sám (nutné tlačítko) – zohlednit u napájení z Delty a v plánu obnovy. (2026-11-18)
- [ ] Instalace OpenWrt do NAND vyžaduje samostatný plán: oddíl ubi v OpenWrt (od 0x140000) přepíše prostředí U-Bootu i všechny oddíly výrobce a uboot_env v OpenWrt (0xC0000) leží uvnitř oddílu původního U-Bootu; buď nový U-Boot z OpenWrt, nebo vlastní mapa oddílů – rozhodnout před M7. (2026-11-18)
- [ ] Při dočasném bootu (M3) nastavit bootargs jen v RAM (setenv bez saveenv): jádro OpenWrt přebírá příkazový řádek od původního U-Bootu (root=/dev/nfs, mtdparts=nand_mtd); podle analýzy je neškodný, na hardwaru neověřeno. Pod OpenWrt s původním U-Bootem nepoužívat fw_setenv. (2026-11-18)
- [ ] Do návrhu backendu: nepovolit sdílení na nepřipojené cestě (zápis by šel do NAND), po změně konfigurace volat sync před hlášením úspěchu, pole a disky adresovat podle UUID – zjištěno v simulaci. (2026-11-18)
- [ ] Datový svazek md0 je jednoduchý (linear) na jediném disku sda2, ne RAID1; při přechodu na OpenWrt rozhodnout, jestli ho založit znovu (je prázdný). (2026-11-19)
- [ ] Při bootu OpenWrt (M3) ověřit, že se modul lm85 naváže na čip na i2c 0x2e a ventilátor reaguje na teplotu; ve stock firmware je LM85 potvrzený. (2026-11-19)
- [ ] Telnet je nešifrovaný (naběhne sám po zapnutí přes háček na md0, ověřeno 2026-11-19); v OpenWrt ho nahradit SSH s klíčem. (2026-11-16)
- [ ] Ověřit zdraví disku pod /dev/md0 (SMART, vadné sektory); vlastník potvrdil, že svazek je prázdný po formátu a lze ho volně použít, zdraví disku tím ale ověřené není. (2026-11-16)
- [ ] Doověřit PCB revizi desky (procesor, takt, síťový čip a čip ventilátoru už potvrdila diagnostika 2026-11-19). (2026-11-16)
- [ ] Vysvětlit rozpor kernel_addr=480000 v exportu prostředí proti začátku kernel_1 na 0xC80000 podle uloženého mtdparts; žádnou hodnotu bez ověření nepoužívat k bootování ani zápisu. (2026-11-16)
- [ ] Zjistit skutečné USB/FAT32 chování bootloaderu, podporu kernelu a DT/ATAG handoff a návrat při selhání; statické záznamy příkazů nejsou funkční test. (2026-11-16)
- [ ] Doplnit původní nástroj a volby zálohování, ECC/OOB a bad-block politiku a kvalifikovat obnovu; devět ověřených hashů a oddělení OOB nepotvrzují obnovitelnost a analýza ECC neopravovala. (2026-11-16)
- [ ] Dokončit offline posouzení původní dokumentace výrobce a komunitních návodů; přímý archiv výrobce a Doozan nebyly dostupné, historické návody nejsou schválený postup. (2026-11-16)
- [ ] Před přijetím ADR ověřit bezpečnostní životní cyklus vybrané větve OpenWrt, potřebné balíčky a rozdíly NSA310 proti profilu NSA310b. (2026-11-16)
- [ ] Změřit rozpočet RAM, velikosti systému a výkonu TLS/SMB/SFTP i možnost bezpečného chlazení; uvedené limity jsou cíle, nikoliv výsledky. (2026-11-16)
- [ ] Upřesnit podepisování aktualizací, důvěryhodné klíče, migrace konfigurace a recovery při výpadku napájení bez předpokladu dvou NAND slotů; mechanismus dosud není implementovaný. (2026-11-16)
- [ ] Začlenit tools/hamaproject.patch do přehledu a ověřit skutečně vydaný portfolio.json po standardním nasazení; dřívější typecheck a místní sběr prošly, 10 HTTP testů blokoval listen EPERM a HamaProject je mimo povolený zápis relace, body jsou zatím pouze lokální; podklad Stage 1 je aktualizován lokálně, skutečné nasazení a ověření webu zůstává nedokončené. (2026-11-16)

## Dokončené etapy
- Čtecí diagnostika na NASu: 88F6281 A1, LM85, RTL8111D sedí na OpenWrt NSA310b; původní jádro bez kexec, Telnet po zapnutí naběhne sám. (2026-11-19)
- Ověřený build OpenWrt 25.12.6 (podpis a SHA-256) a simulace v QEMU: ARMv5TE userspace bez chyb, plný systém v 256 MB s RAID1/ext4/SMB, reboot a výpadek napájení; opraveno pořadí mdadm/fstab a ukázkové pole v konfiguraci mdadm. (2026-11-18)
- Stage 1 vyhodnocen a zastaven: build uClibc-ng 1.0.59 narazil na glibc podmínku v linux/socket.h z 2.6.31.8; rozbité buildy a cache smazány, další směr OpenWrt. (2026-11-16)
- Po opakovaných pádech VM (příčina na straně Proxmoxu vyřešena) obnoven vynulovaný checkout Buildrootu a poškozený git repozitáře, smazány rozbité buildy a cache. (2026-11-16)
- Opraven Stage 1 build, který zamrazil VM (linux.git klonovaný do tmpfs /tmp): hlavičky z kernel.org tarballu s hashem, zákaz tmpfs cest a -j podle volné RAM. (2026-11-16)
- Založen dokumentační bootstrap, ADR, návrh HAL, bezpečnostní model a plán obnovy bez zásahu do zařízení. (2026-11-16)
- Dokončena lokální analýza OOB struktury a U-Bootu, zapsán root Telnet a ověřené hardwarové údaje a posouzeny možnosti bootu bez UART; originální mtd0 nezměněn. (2026-11-16)
- Připraven Stage 1 build postup a ruční chroot/SSH controller; 14 bezpečnostních a ABI kontrol prošlo lokálně, úplný build a NAS testy zůstávají neověřené. (2026-11-16)
