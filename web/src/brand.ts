/** The product name lives here only. Rename the product by changing it. */
export const Name = 'Ervisio';
/** The product was called LinuxAdmin up to 0.2.0 (see lib/storageMigration.ts for what that changes here). */
export const FormerName = 'LinuxAdmin';
export const brand = { name: Name, cookie: 'ervisio_session', storagePrefix: 'ervisio.', csrf: 'ervisio' } as const;
