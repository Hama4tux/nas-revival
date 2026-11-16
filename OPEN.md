# Otevřené body

## Stav (2026-11-16)
- Stav: Dokončen dokumentační bootstrap NAS Revival a lokální Git repozitář; OpenWrt je podmíněný kandidát, kompatibilita konkrétního NSA310 není ověřená. Firmware ani aplikační služby nejsou implementované.
- Další krok: Dokončit porovnání zdrojů a připravit evidenční formulář pro přesnou revizi NSA310; přístup k zařízení a zálohy provést až v samostatně schválené fázi.

## Čeká na rozhodnutí
- [ ] Potvrdit, zda vložený SATA disk obsahuje důležitá data a zda zahájená inicializace JBOD může znamenat potřebu samostatné záchrany dat. (2026-11-16)
- [ ] Před připojením UART, restartem nebo USB bootem schválit samostatnou fázi inventury a způsob ochrany zařízení a dat. (2026-11-16)

## Nedodělané
- [ ] Ověřit fyzickou revizi NSA310, SoC, RAM, NAND geometrii, síťový čip a řadič ventilátoru; upstream Device Tree není měření konkrétního kusu. (2026-11-16)
- [ ] Zjistit bootloader, dostupné bootovací příkazy, formát kernelu a skutečné rozložení MTD; OpenWrt používá jiné oddíly než původní schéma. (2026-11-16)
- [ ] Připravit a později ověřit zálohy NAND s ECC/OOB a bad-block metadaty i návratový postup; dosud neexistuje ověřená obnovitelná záloha. (2026-11-16)
- [ ] Dokončit offline posouzení původní dokumentace výrobce a komunitních návodů; přímý archiv výrobce a Doozan nebyly dostupné, historické návody nejsou schválený postup. (2026-11-16)
- [ ] Před přijetím ADR ověřit bezpečnostní životní cyklus vybrané větve OpenWrt, potřebné balíčky a rozdíly NSA310 proti profilu NSA310b. (2026-11-16)
- [ ] Změřit rozpočet RAM, velikosti systému a výkonu TLS/SMB/SFTP i možnost bezpečného chlazení; uvedené limity jsou cíle, nikoliv výsledky. (2026-11-16)
- [ ] Upřesnit podepisování aktualizací, důvěryhodné klíče, migrace konfigurace a recovery při výpadku napájení; žádný aktualizační mechanismus dosud není implementovaný. (2026-11-16)
- [ ] Začlenit tools/hamaproject.patch do přehledu a ověřit skutečně vydaný portfolio.json po standardním nasazení; patch, typecheck a místní sběr ověřeny v dočasné kopii, ale 10 HTTP testů blokuje listen EPERM a HamaProject je mimo povolený zápis relace, body jsou zatím pouze lokální. (2026-11-16)

## Dokončené etapy
- Připraven dokumentační bootstrap NAS Revival, podmíněné ADR distribuce, hardwarová evidence, bezpečnostní model a plán obnovy bez zásahu do zařízení. (2026-11-16)
- [ ] Navrhnout konkrétní podepisování a ukládání aktualizací, kompatibilitu konfigurace a recovery při výpadku napájení bez předpokladu dvou NAND slotů. (2026-11-16)
- [ ] Dokončit přenos návrhu NAS Revival do HamaProject/data/projects.ts a CHANGELOG.md, požadované kontroly, standardní nasazení a kontrolu skutečně vydaného portfolio.json; cílový projekt leží mimo zapisovatelné kořeny sandboxu a body jsou pouze lokálně. (2026-11-16)

## Dokončené etapy
- Založen dokumentační základ, technická analýza, návrh HAL a plán experimentů bez zásahu do NASu. (2026-11-16)
