// Mirrors @grafana/ui's internal test-utils/mockDom.ts (not published to npm).
// jsdom reports all-zero element rects, which sends floating-ui's positioning
// loop into a flushSync storm ("Too many re-renders") when a Combobox menu
// opens. Mocking non-zero measurements lets the loop converge.
export function mockBoundingClientRect(rect: Partial<DOMRect> = {}): void {
  const defaults: DOMRect = {
    width: 400,
    height: 400,
    top: 0,
    left: 0,
    bottom: 400,
    right: 400,
    x: 0,
    y: 0,
    toJSON: () => {},
  };

  const merged = { ...defaults, ...rect };

  Object.defineProperty(Element.prototype, 'getBoundingClientRect', {
    value: () => merged,
    configurable: true,
  });

  Object.defineProperty(HTMLElement.prototype, 'offsetWidth', {
    get: () => merged.width,
    configurable: true,
  });
  Object.defineProperty(HTMLElement.prototype, 'offsetHeight', {
    get: () => merged.height,
    configurable: true,
  });
}

export function mockComboboxRect() {
  mockBoundingClientRect({ width: 120, height: 120 });
}
