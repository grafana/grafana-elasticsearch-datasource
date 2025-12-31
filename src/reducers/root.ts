import { ReducersMapObject } from '@reduxjs/toolkit';
import { Action as AnyAction, combineReducers } from 'redux';
import { cleanUpAction } from './actions/cleanUp';

const addedReducers = {};

export const addReducer = (newReducers: ReducersMapObject) => {
  Object.assign(addedReducers, newReducers);
};

export const createRootReducer = () => {
  const appReducer = combineReducers({
    ...addedReducers,
  });

  return (state: Parameters<typeof appReducer>[0], action: AnyAction) => {
    if (action.type !== cleanUpAction.type) {
      return appReducer(state, action);
    }

    const { cleanupAction } = action.payload;
    cleanupAction(state);

    return appReducer(state, action);
  };
};
