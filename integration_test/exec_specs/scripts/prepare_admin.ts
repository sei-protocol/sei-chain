/**
 * Create and fund an ephemeral EEST seed account on the local Docker devnet.
 *
 * This intentionally reuses the integration suite's existing admin bootstrap
 * helpers. The mnemonic exists only in this process; only the derived private
 * key is returned to the calling CI step.
 */
import {
    fundAdminOnSei,
    generateMnemonic,
} from '../../precompile_tests/utils/cosmosUtils';
import { EvmAccount } from '../../precompile_tests/utils/evmUtils';
import { seiRpc } from '../../precompile_tests/utils/chainUtils';

async function main(): Promise<void> {
    const mnemonic = await generateMnemonic();
    const provider = seiRpc();
    const admin = EvmAccount.fromMnemonic(mnemonic, provider);

    await fundAdminOnSei(admin.address, mnemonic, provider);
    process.stdout.write(admin.wallet.privateKey);
}

main().catch((error: unknown) => {
    console.error(error);
    process.exitCode = 1;
});
