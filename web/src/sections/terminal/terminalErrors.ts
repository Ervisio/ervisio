import { ApiError } from '../../api';

export const errMsg = (e: unknown) => (e instanceof ApiError ? e.message : String(e));
