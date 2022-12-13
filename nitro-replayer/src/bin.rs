use nitro_replayer::replay::*;

pub fn main() {
    let accounts: Vec<ByteSliceView> = vec![];
    let sysvar_accounts: Vec<ByteSliceView> = vec![];
    let programs: Vec<ByteSliceView> = vec![];
    let transactions: Vec<ByteSliceView> = vec![];
    let output_directory = ByteSliceView::from_str("/Users/tonychen/repos/nitro-replayer/investigation/");
    replay(
        FilePaths::from_vec(&accounts),
        FilePaths::from_vec(&sysvar_accounts),
        FilePaths::from_vec(&programs),
            FilePaths::from_vec(&transactions),
        output_directory,
    );
}
