/** A DataTransfer for drag tests: jsdom's drag events carry none. Its effects are plain fields, which handlers set as a browser's would. */
export function dataTransfer(): DataTransfer {
  const data = new Map<string, string>();
  return {
    dropEffect: 'none',
    effectAllowed: 'uninitialized',
    get types() {
      return [...data.keys()];
    },
    setData: (type: string, value: string) => {
      data.set(type, value);
    },
    getData: (type: string) => data.get(type) ?? '',
  } as unknown as DataTransfer;
}
