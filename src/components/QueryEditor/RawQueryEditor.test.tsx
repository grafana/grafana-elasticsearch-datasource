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

// Mirror the real monaco enum values so the two run shortcuts map to distinct keybindings.
const KeyMod = { CtrlCmd: 2048, Shift: 1024 };
const KeyCode = { Enter: 3 };

describe('RawQueryEditor', () => {
  beforeEach(() => {
    capturedOnEditorDidMount = undefined;
  });

  function mountEditor() {
    const onChange = jest.fn();
    const onRunQuery = jest.fn();
    render(<RawQueryEditor value="old" language="esql" onChange={onChange} onRunQuery={onRunQuery} />);

    const commands = new Map<number, () => void>();
    const editor = {
      addCommand: (keybinding: number, cb: () => void) => {
        commands.set(keybinding, cb);
      },
      onDidFocusEditorText: jest.fn(),
      onDidContentSizeChange: jest.fn(),
      getContentHeight: () => 0,
      layout: jest.fn(),
      getValue: () => 'edited but not blurred',
      getAction: () => ({ run: jest.fn() }),
    };

    capturedOnEditorDidMount?.(editor, { KeyMod, KeyCode });
    return { onChange, onRunQuery, commands };
  }

  it.each([
    ['Shift+Enter', KeyMod.Shift | KeyCode.Enter],
    ['Ctrl/Cmd+Enter', KeyMod.CtrlCmd | KeyCode.Enter],
  ])('commits the current editor value before running on %s', (_name, keybinding) => {
    const { onChange, onRunQuery, commands } = mountEditor();

    const runCommand = commands.get(keybinding);
    expect(runCommand).toBeDefined();
    runCommand!();

    expect(onChange).toHaveBeenCalledWith('edited but not blurred');
    expect(onRunQuery).toHaveBeenCalledTimes(1);
    // The current contents must be committed before the query runs, so keyboard submit and the
    // Run query button validate and send the same text.
    expect(onChange.mock.invocationCallOrder[0]).toBeLessThan(onRunQuery.mock.invocationCallOrder[0]);
  });
});
