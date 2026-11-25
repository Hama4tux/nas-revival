<!-- SPOLECNY-POSTUP:START – shodný text v itadmin-toolkit, Silo a HR-Candidate-Management. Měň ho ve všech třech projektech najednou. -->
# Společný postup pro AI agenty (Claude Code i ChatGPT/Codex)

Obsah: 1–7 git, commity, push, OPEN.md a předávání práce · 8 seniorní přístup a respektování stacku · 9 bezpečnost.

Platí stejně pro každého agenta. Codex čte tento soubor přímo, Claude Code přes import v `CLAUDE.md`. Projektová pravidla (stack, architektura, kontroly, nasazení) jsou v `CLAUDE.md` – Codex si ho přečte taky. Pořadí priority: výslovný pokyn vlastníka → pravidla projektu (`CLAUDE.md`, sekce „Tento projekt“ níže) → tento postup.

## 1. Začátek práce
1. `git fetch` a `git status -sb`. Vlastník pracuje na více strojích (Linux, Mac, Windows) a s více agenty – vždy počítej s tím, že na GitHubu může být něco nového.
2. Když je větev pozadu: `git pull --ff-only`. Když to nejde, protože máš i lokální commity, postupuj podle bodu 5.
3. Necommitnuté změny, které nejsou tvoje, nepřepisuj, nemaž, nestashuj a nepřidávej do svých commitů. Řekni vlastníkovi, co v pracovním stromu leží, a pracuj kolem nich.
4. Přečti `OPEN.md`, roadmapu (`docs/ROADMAP.md`), `git log --oneline -10` a poslední záznamy v `CHANGELOG.md`, ať navážeš na práci druhého agenta a nepřepíšeš ji.

## 2. Během práce
- Pracuje se přímo na `main` (bez větví a pull requestů, pokud vlastník neřekne jinak).
- Jeden commit = jedna ucelená změna, kterou jde samostatně pochopit a vrátit.
- Před commitem musí projít kontroly projektu (testy, typecheck, build – viz „Tento projekt“). Když je v daném prostředí spustit nejde (např. Swift na Linuxu), smíš commitnout, ale napiš to vlastníkovi a do `OPEN.md` jako „neověřeno“.
- Dokumentace patří do stejného commitu jako změna: záznam v `CHANGELOG.md` vždy (nadpis `## RRRR-MM-DD`, nejnovější nahoře, česky, srozumitelně pro uživatele), odškrtnutí v roadmapě a další dokumenty podle `CLAUDE.md`.

## 3. Commit
- Přidávej konkrétní soubory (`git add <soubory>`), ne `git add -A` ani `git add .` – do commitu se nesmí dostat cizí rozdělané změny.
- Zpráva anglicky ve tvaru `Oblast: co se změnilo` (např. `RDP: arrow keys and Ctrl shortcuts on the on-screen keyboard`), v těle volitelně proč.
- Poslední řádek zprávy říká, který agent změnu udělal:
  - Claude Code: `Co-Authored-By: Claude <model> <noreply@anthropic.com>`
  - ChatGPT/Codex: `Co-Authored-By: Codex <noreply@openai.com>`
- Nikdy do gitu: hesla, tokeny, klíče, obsah `.env`, osobní údaje, lokální konfiguraci (`*.local.*`) a výstupy buildu.

## 4. Push a konec práce
- Hotovou a ověřenou změnu commitni a **hned pushni** (`git push`), nečekej na konec dne. Kde push zároveň nasazuje, platí pravidla nasazení projektu (viz „Tento projekt“).
- Na konci práce musí platit: žádné tvoje necommitnuté změny a `git status -sb` ukazuje `main...origin/main` bez „ahead“ a „behind“.
- Nedokončenou práci, která by rozbila build nebo nasazení, necommituj. Zapiš do `OPEN.md`, co zůstalo rozdělané, **na kterém stroji** (Linux / Mac / Windows) a v jakých souborech, a řekni to vlastníkovi.
- Projekt bez vzdáleného repozitáře (`git remote -v` je prázdné): commituj, push vynech.

## 5. Konflikty a zakázané operace
- Když se lokální a vzdálená větev rozešly: `git pull --rebase` (přeskládá jen tvoje dosud nepushnuté commity). Konflikt řeš tak, aby zůstaly obě změny. Když nevíš, která verze je správná, zastav se a zeptej se vlastníka.
- Bez výslovného pokynu vlastníka je zakázáno: `git push --force`, `git reset --hard`, `git checkout -- .` / `git restore .`, `git clean`, stash cizích změn, přepis historie pushnutých commitů, mazání větví a úprava `.gitignore`, která by schovala cizí soubory.

## 6. Otevřené body → OPEN.md
- Vše nedořešené patří do kořenového `OPEN.md`: otázka nebo rozhodnutí pro vlastníka, odložený či nedokončený úkol, neověřená změna, zjištěné riziko, krok, který musí udělat vlastník sám.
- Formát: sekce `## Čeká na rozhodnutí` a `## Nedodělané`, jedna česká odrážka na řádek `- [ ] text (RRRR-MM-DD)`, srozumitelná bez kontextu konverzace. Vyřešené odškrtni `[x]` nebo smaž. Plány patří do roadmapy, ne sem. Nikdy hesla, tokeny ani osobní údaje.
- Na začátku `OPEN.md` drž sekci `## Stav (RRRR-MM-DD)` se dvěma odrážkami `- Stav: …` (1–2 věty, kde projekt teď je) a `- Další krok: …`. Po dokončení ucelené etapy je přepiš a aktualizuj datum. Přehled http://projects.home je zobrazí místo ručního popisu a od 5 commitů po tomto datu hlásí „Stav zastaralý“.
- Dokončenou etapu zapiš jedním řádkem do sekce `## Dokončené etapy` (`- výsledek (RRRR-MM-DD)`), přehled ji ukáže mezi milníky.
- `OPEN.md` se commituje a pushuje jako ostatní soubory.
- Po změně `OPEN.md` obnov přehled http://projects.home: spusť `deploy/deploy.sh` v `~/VScode/HamaProject` (Node přes nvm, jen z Linuxu v domácí síti, smíš bez ptaní). Kde to nejde (Mac, Windows), řekni vlastníkovi, že se body ukážou po příštím nasazení.

## 7. Předání mezi agenty a vlastníkovi
- Druhý agent nevidí tvoji konverzaci. Stav práce musí jít vyčíst jen z gitu, `CHANGELOG.md`, roadmapy a `OPEN.md`.
- Na konci odpovědi vlastníkovi stručně uveď: co se změnilo, commit(y), jestli je pushnuto a nasazeno, co zůstalo v `OPEN.md`.

## 8. Přístup k práci – jako seniorní vývojář
- **Nejdřív pochopit, pak měnit.** Před úpravou si přečti dotčený kód, jeho volající a testy a dokumentaci projektu. Navazuj na existující vzory, názvy a strukturu; nevymýšlej paralelní řešení k něčemu, co v projektu už je.
- **Respektuj stack projektu** (popsaný v `CLAUDE.md` a dokumentaci architektury). Bez souhlasu vlastníka neměň jazyk, framework, databázi, build ani strukturu projektu. Novou závislost přidej jen, když je opravdu potřeba, a její důvod a licenci zapiš do dokumentace architektury projektu; dej přednost tomu, co už projekt používá, a standardní knihovně.
- **Dělej, co bylo zadáno.** Změna má jasný rozsah; bez domluvy nerefaktoruj nesouvisející kód, nepřejmenovávej a nepřeformátovávej celé soubory. Vylepšení, které tě napadne mimo zadání, navrhni vlastníkovi nebo zapiš do `OPEN.md`.
- **Kvalita kódu:** čitelný a jednoduchý kód před chytrým; malé funkce s jasnou odpovědností; žádný mrtvý kód, zakomentované bloky ani ladicí výpisy v commitu. Komentáře vysvětlují proč, ne co. Chyby se ošetřují a hlásí srozumitelně, nikdy se tiše nepolykají.
- **Testy:** nová logika a opravená chyba mají test (u chyby nejdřív test, který ji zachytí). Nesmazávej ani neoslabuj testy, aby prošly; nevypínej kontroly typů ani linteru (`any`, `// @ts-ignore`, `!` v Swiftu, `--no-verify`) bez zdůvodnění v kódu.
- **Kompatibilita a data:** mysli na existující data a uživatele – migrace DB jen dopředné a bez ztráty dat, změna API nebo uloženého formátu se zpětnou kompatibilitou nebo převodem. Destruktivní operace (mazání dat, drop tabulek, přepis produkce) jen po výslovném souhlasu vlastníka a se zálohou.
- **Výkon a provoz:** vyhýbej se zbytečným dotazům v cyklech, neomezeným seznamům a blokování hlavního vlákna; u síťových operací timeouty a srozumitelné chyby.
- **UI:** texty podle pravidel lokalizace projektu, ovladatelnost na všech podporovaných zařízeních (telefon, tablet, desktop), přístupnost (popisky, ovládání klávesnicí, kontrast).
- **Poctivost:** nikdy netvrď, že je něco otestované, nasazené nebo funkční, když jsi to neověřil. Napiš přesně, co prošlo, co ne a co nešlo spustit. Když si nejsi jistý zadáním nebo dopadem rozhodnutí, zeptej se dřív, než začneš – u věcí s jasným standardním řešením rozhodni sám a uveď to.

## 9. Bezpečnost (minimum v každém projektu)
- **Tajné údaje:** hesla, tokeny, klíče a certifikáty jen v bezpečném úložišti projektu (Keychain, `.env` mimo git, konfigurace na serveru). Nikdy v kódu, gitu, logu, chybové hlášce, testech ani v odpovědi vlastníkovi; nevypisuj je ani při ladění.
- **Osobní údaje** (kandidáti, hráči, klienti): jen tam, kde jsou nutné, nikdy v logu, commitu, testovacích datech ani dokumentaci – v testech jen smyšlená data. Data se neposílají do cizích služeb ani AI bez souhlasu vlastníka.
- **Vstupy jsou nedůvěryhodné:** všechno zvenku (formuláře, API, soubory, e-maily, síť, URL) validuj na serveru podle schématu. Databáze jen parametrizovanými dotazy nebo ORM, žádné skládání SQL ani příkazů shellu z řetězců. Výstup do HTML escapuj. Nahrané soubory omez velikostí a typem a neukládej je pod uživatelem zadanými cestami.
- **Oprávnění:** autorizaci kontroluj na serveru u každé akce (ne jen schováním tlačítka v UI), filtruj data podle uživatele už v dotazu. Nejmenší nutná oprávnění pro uživatele, služby i databázové účty.
- **Hesla a relace:** hesla jen hashovaná osvědčeným algoritmem (argon2/bcrypt/scrypt), cookies `HttpOnly` + `Secure` + `SameSite`, ochrana proti CSRF a omezení počtu pokusů (rate limiting).
- **Síť:** šifrované spojení (TLS/SSH); ověřování certifikátů a klíčů hostitele nevypínej – výjimku musí vědomě potvrdit uživatel. Žádné odesílání dat do cloudu nad rámec toho, co projekt výslovně dovoluje.
- **Závislosti:** jen udržované balíčky z oficiálních zdrojů se zamčenými verzemi (lockfile); při přidání nebo aktualizaci zkontroluj známé zranitelnosti (`npm audit` apod.).
- **Chyby a logy:** uživateli neukazuj stack trace ani vnitřní detaily; do logu zapisuj události bez tajných a osobních údajů. Bezpečnostní prvky (audit, šifrování, ověřování, CSP) neobcházej ani dočasně.
- **Zjištěné riziko**, které se teď neopravuje, zapiš do `OPEN.md` a řekni ho vlastníkovi; nic bezpečnostně citlivého neřeš potichu.
<!-- SPOLECNY-POSTUP:END -->

# Tento projekt – NAS Revival

- **Přístup k NASu:** `ssh nsa310` (root, klíč; adresa je jen v `~/.ssh/config` pracovní stanice, do repa ji nepiš). NAS běží na Debianu 13 a vlastník od 2026-11-25 práci přímo na něm povoluje (služby, konfigurace, nasazení GUI); zápisy do NAND a U-Boot prostředí a restarty dál jen po výslovném souhlasu. Starší body níže o zákazu přístupu a fázi OpenWrt jsou překonané. Sériová konzole (CP2102) je na samostatném notebooku s Live Ubuntu, ne na pracovní stanici.

- Komunikace s vlastníkem česky, veřejná technická dokumentace a commity anglicky; CHANGELOG.md a OPEN.md česky podle společného postupu.
- Bootstrap je dokončen. Stage 1 (chroot userspace na původním kernelu) je od 2026-11-16 zastavený rozhodnutím vlastníka; recept zůstává v userspace/stage1 a scripts/stage1 jen pro případný návrat, dál ho nerozvíjet. Aktuální fáze je kvalifikace OpenWrt: veřejný průzkum, příprava čtecích diagnostických příkazů a plánu dočasného bootu. Žádný flash, zápis do NAND/U-Boot prostředí, kexec, webové UI ani NAS služby.
- Repozitář je zatím pouze lokální na main, bez remote. V tomto stavu fetch/pull/push vynechat a uvést to při předání. Nevytvářet vzdálený repozitář. Po jeho pozdějším zřízení platí společný git postup.
- Autoritativní plán je docs/roadmap.md (malými písmeny), aktuální stav a otázky jsou v kořenovém OPEN.md.
- Předpokládaný cíl: původní ZyXEL NSA310. NSA310b/NSA310S ani různé DT nelze zaměňovat bez ověření. Každý parametr má oddělený stav zdroj / hlášení vlastníka / měření / neznámé.
- Zákaz přístupu k živému NASu, skenování sítě, připojování UART/USB, restartů, flashování NAND/MTD, změn U-Boot prostředí a formátování či opravy disků v této fázi. Jedinou výjimkou je předem vlastníkem schválený seznam čistě čtecích příkazů přes stávající root Telnet; každý běh zvlášť schválený. Postupy v docs/recovery jsou návrhy, nikoliv oprávnění je vykonat.
- Žádná instalace nebo změna hostitelského systému bez potřeby a souhlasu. Veřejný zdrojový průzkum a místní dokumentační kontroly jsou povolené.
- OpenWrt je podmíněný kandidát podle ADR-0001, nikoliv potvrzená podpora konkrétního kusu. Žádné univerzální flash offsety, přihlašovací údaje ani zkopírované komunitní instalační příkazy.
- Embedded-first: měřit RAM/CPU/velikost. Na NAS nepřidávat automaticky Node.js, Docker nebo databázový server. Frontend bude pouze statický; API a hardwarové adaptéry oddělit.
- Využívat upstream ovladače, hwmon/LED/input rozhraní a důvěryhodné profily; žádné přímé GPIO/registr/NAND zápisy z webového API.
- Nový původní obsah je MIT. Cizí komponenty zachovávají licence; před binární distribucí provést kontrolu zdrojových povinností a SBOM.
- Kontroly dokumentačních změn: existující relativní odkazy, čitelnost JSON/YAML evidence, zákazy zápisů v hardware.yaml a git diff --check. Neoznačovat tyto kontroly jako testy na NASu.
- Neupravovat rodičovský nas_proxy.py ani jiné cizí soubory. Nezveřejňovat IP/MAC, sériová čísla, NAND dumpy, certifikáty nebo environment bootloaderu.
- HamaProject může obsahovat jen vývoj produktu NAS Revival, nikdy provozní údaje domácího NASu. Respektovat sandbox; pokud nelze aktualizovat přehled, zapsat lokální podklad a nepravdivě nehlásit nasazení.
