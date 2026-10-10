/**
 * Dump how dotenv (the TS oracle's .env loader) parses tricky env files.
 *
 * The Go oracle ports dotenv's parser; its tests compare it against this
 * output, so a .env file means the same thing to both oracles.
 *
 * Usage (from oracle/):
 *   npx tsx scripts/dump-dotenv-cases.ts > internal/config/testdata/dotenv-cases.json
 */
import { createRequire } from "node:module";
import dotenv from "dotenv";

const { version } = createRequire(import.meta.url)("dotenv/package.json");

const CASES: Record<string, string> = {
  basic: "A=1\nB=two\n",
  "no final newline": "A=1",
  "spaces around the equals sign": "A = 1\nB=  spaced out  \nC\t=\ttabs\t\n",
  "export prefix": "export A=1\nexport  B=2\n",
  "colon syntax": "A: 1\nB:2\n",
  comments:
    "# a comment\nA=1 # trailing comment\nB=2#no space\nC=http://host/path#fragment\n  # indented comment\n",
  "hash inside quotes":
    "A=\"1 # not a comment\"\nB='2 # nor this'\nC=`3 # nor this`\n",
  quotes:
    "A='single'\nB=\"double\"\nC=`backtick`\nD=  'padded'  \nE=\"x\" # comment\n",
  "escaped quotes": "A=\"a\\\"b\"\nB='a\\'b'\n",
  "newline escapes":
    'A="l1\\nl2\\rl3"\nB=\'l1\\nl2\'\nC=l1\\nl2\nD="unclosed\\n\n',
  "multi-line values": "A=\"line 1\nline 2\"\nB='line 1\nline 2'\nC=after\n",
  "quote opened on one line, closed on another": "A='abc\nB=def'\nC=ghi\n",
  "unclosed quotes": "A=\"abc\nB='def\nC=`ghi\n",
  "empty values": "A=\nB=\"\"\nC=''\nD=   \nE=``\n",
  "lines dotenv skips":
    "some note without equals\nA=1\n=nokey\n   \nA B=2\nC=3\n",
  "keys with dots and dashes": "MY-NOTE=1\nmy.key=2\n_UNDER=3\n",
  "byte order mark": "\ufeffA=1\nB=2\n",
  "no-break space before a key": "\u00a0A=1\n",
  "CRLF and CR line endings": "A=1\r\nB=2\r\nC=3\rD=4\r\n",
  "dollar signs are kept": 'A=abc$def\nB=${A}\nC="$HOME"\n',
  "equals signs in values": "A=b=c\nB==d\n",
  backslashes: "A=\"C:\\\\path\\\\to\"\nB=C:\\path\nC='C:\\path'\n",
  "later keys win": "A=1\nA=2\n",
  "values with spaces": "A=1 2 3\nB=  a  b  \n",
  "a private key":
    "PRIVATE_KEY=0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80\n",
  "cron expressions":
    'SAFE_VALUE_CRON="0 */10 * * * *"\nSPENDING_ORACLE_CRON=*/5 * * * *\n',
  unicode: 'A=héllo wörld\nB="日本語"\n',
  "blank lines between": "\n\nA=1\n\n\nB=2\n\n",
  // JavaScript trims U+FEFF and U+3000 but not U+0085 (NEL), unlike Go.
  "unusual whitespace": "A=x\ufeff\nB=y\u0085\nC=\u3000z\u3000\n",
};

const out = Object.entries(CASES).map(([name, input]) => ({
  name,
  input,
  parsed: dotenv.parse(input),
}));
console.log(JSON.stringify({ dotenv: version, cases: out }, null, 2));
