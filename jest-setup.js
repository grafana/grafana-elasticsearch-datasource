// Jest setup provided by Grafana scaffolding
import { MessageChannel } from 'node:worker_threads';
import React from 'react';
import './.config/jest-setup';
import { matchers } from './src/test/matchers';

global.React = React;

// Override the scaffolded canvas stub (returns undefined) so Combobox can call getContext('2d').measureText()
HTMLCanvasElement.prototype.getContext = () => ({
  measureText: () => ({ width: 0 }),
  font: '',
});

// The following mocks are copied from @grafana/plugin-configs jest/jest-setup.js
// (not published to npm, only available in the grafana monorepo).

Object.defineProperty(document, 'fonts', {
  value: { ready: Promise.resolve({}) },
});

// Used by useMeasure. Without this, Combobox falls back to canvas-based text measurement and crashes.
global.ResizeObserver = class ResizeObserver {
  static #observationEntry = {
    contentRect: {
      x: 1,
      y: 2,
      width: 500,
      height: 500,
      top: 100,
      bottom: 0,
      left: 100,
      right: 0,
    },
    target: {
      // Needed for react-virtual to work in tests
      getAttribute: () => 1,
    },
  };

  #isObserving = false;
  #callback;

  constructor(callback) {
    this.#callback = callback;
  }

  #emitObservation() {
    setTimeout(() => {
      if (!this.#isObserving) {
        return;
      }

      this.#callback([ResizeObserver.#observationEntry], this);
    });
  }

  observe() {
    this.#isObserving = true;
    this.#emitObservation();
  }

  disconnect() {
    this.#isObserving = false;
  }

  unobserve() {
    this.#isObserving = false;
  }
};

// originally using just global.MessageChannel = MessageChannel
// however this results in open handles in jest tests
// see https://github.com/facebook/react/issues/26608#issuecomment-1734172596
global.MessageChannel = class {
  constructor() {
    const channel = new MessageChannel();
    this.port1 = new Proxy(channel.port1, {
      set(port1, prop, value) {
        const result = Reflect.set(port1, prop, value);
        if (prop === 'onmessage') {
          port1.unref();
        }
        return result;
      },
    });
    this.port2 = channel.port2;
  }
};

// mock the intersection observer and just say everything is in view
const mockIntersectionObserver = jest.fn().mockImplementation((callback) => ({
  observe: jest.fn().mockImplementation((elem) => {
    callback([{ target: elem, isIntersecting: true }]);
  }),
  unobserve: jest.fn(),
  disconnect: jest.fn(),
}));
global.IntersectionObserver = mockIntersectionObserver;

expect.extend(matchers);
