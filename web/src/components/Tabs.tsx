// A row of tabs over one panel, the chosen one marked.
export interface Tab<T extends string> {
  readonly id: T;
  readonly name: string;
}

export function Tabs<T extends string>({
  label,
  tabs,
  chosen,
  onChoose,
}: {
  readonly label: string;
  readonly tabs: readonly Tab<T>[];
  readonly chosen: T;
  readonly onChoose: (id: T) => void;
}) {
  return (
    <div className="inventory-tabs" role="tablist" aria-label={label}>
      {tabs.map((t) => (
        <button
          key={t.id}
          type="button"
          role="tab"
          aria-selected={t.id === chosen}
          className={`btn btn-quiet${t.id === chosen ? ' tab-active' : ''}`}
          onClick={() => onChoose(t.id)}
        >
          {t.name}
        </button>
      ))}
    </div>
  );
}
