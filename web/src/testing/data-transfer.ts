/** A DataTransfer for drag tests: jsdom's drag events carry none. */
export function dataTransfer(): DataTransfer {
  const data = new Map<string, string>();
  return {
    setData: (type: string, value: string) => {
      data.set(type, value);
    },
    getData: (type: string) => data.get(type) ?? '',
  } as unknown as DataTransfer;
}
