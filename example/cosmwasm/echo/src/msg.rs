use cosmwasm_schema::{QueryResponses, cw_serde};

#[cw_serde]
pub struct InstantiateMsg {}

#[cw_serde]
pub enum ExecuteMsg {
    Echo {
        message: String,
    },
}

#[cw_serde]
#[derive(QueryResponses)]
pub enum QueryMsg {
    #[returns(InfoResponse)]
    Info {},
}

#[cw_serde]
pub struct InfoResponse {
    pub message: String,
}