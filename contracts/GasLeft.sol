pragma solidity ^0.8;

contract GasLeft {

    function getGasLeft()
        external
        returns (uint256)
    {
        return gasleft();
    }
}