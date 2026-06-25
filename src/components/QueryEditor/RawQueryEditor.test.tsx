import { render } from '@testing-library/react';
import React from 'react';

import { RawQueryEditor } from './RawQueryEditor';

type EditorDidMount = (editor: unknown, monaco: unknown) => void;

let capturedOnEditorDidMount: EditorDidMount | undefined;

jest.mock('@grafana/ui', () => {
  const actual = jest.requireActual('@grafana/ui');
  return {
    ...actual,
    CodeEditor: (props: { onEditorDidMount?: EditorDidMount }) => {
      capturedOnEditorDidMount = props.onEditorDidMount;
      return null;
    },
  };
});

describe('RawQueryEditor', () => {
  beforeEach(() => {
    capturedOnEditorDidMount = undefined;
  });

  it('commits the current editor value before running on Ctrl/Cmd+Enter', () => {
    const onChange = jest.fn();
    const onRunQuery = jest.fn();
    render(<RawQueryEditor value="old" language="esql" onChange={onChange} onRunQuery={onRunQuery} />);

    let runCommand: (() => void) | undefined;
    const editor = {
      addCommand: (_keybinding: number, cb: () => void) => {
        runCommand = cb;
      },
      onDidFocusEditorText: jest.fn(),
      onDidContentSizeChange: jest.fn(),
      getContentHeight: () => 0,
      layout: jest.fn(),
      getValue: () => 'edited but not blurred',
      getAction: () => ({ run: jest.fn() }),
    };
    const monaco = { KeyMod: { CtrlCmd: 2048 }, KeyCode: { Enter: 3 } };

    capturedOnEditorDidMount?.(editor, monaco);
    runCommand?.();

    expect(onChange).toHaveBeenCalledWith('edited but not blurred');
    expect(onRunQuery).toHaveBeenCalledTimes(1);
    // The current contents must be committed before the query runs, so keyboard submit and the
    // Run query button validate and send the same text.
    expect(onChange.mock.invocationCallOrder[0]).toBeLessThan(onRunQuery.mock.invocationCallOrder[0]);
  });
});
