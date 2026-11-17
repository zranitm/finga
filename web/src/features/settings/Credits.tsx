// The credits (skill-icons.md section 7): game-icons.net's CC BY 3.0 asks for each author's name; the CC0 model packs are thanked as a courtesy.
import { skills } from '@/gamedata/skills';

/** Each author whose icon a skill uses, from the icons' paths, author/name, as game-icons.net names them: lorc is Lorc. */
const iconAuthors = [
  ...new Set(skills.flatMap((s) => s.stages.map((st) => st.icon.split('/')[0]!))),
]
  .sort()
  .map((a) =>
    a.replace(
      /(^|-)([a-z])/g,
      (_, dash: string, c: string) => `${dash ? ' ' : ''}${c.toUpperCase()}`,
    ),
  );

/** "A, B and C". */
function listed(names: readonly string[]): string {
  return names.length < 2
    ? names.join('')
    : `${names.slice(0, -1).join(', ')} and ${names.at(-1)!}`;
}

export function Credits() {
  return (
    <section aria-label="Credits">
      <h2>Credits</h2>
      <p className="credits">
        Skill icons from{' '}
        <a href="https://game-icons.net" target="_blank" rel="noreferrer">
          game-icons.net
        </a>{' '}
        by {listed(iconAuthors)}, under{' '}
        <a href="https://creativecommons.org/licenses/by/3.0/" target="_blank" rel="noreferrer">
          CC BY 3.0
        </a>
        .
      </p>
      <p className="credits">
        3D models by Kay Lousberg (KayKit) and Quaternius, in the public domain under CC0.
      </p>
    </section>
  );
}
